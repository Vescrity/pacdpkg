#include "bridge.h"

#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

#ifdef LIBAPT

#include <apt-pkg/init.h>
#include <apt-pkg/configuration.h>
#include <apt-pkg/cachefile.h>
#include <apt-pkg/policy.h>
#include <apt-pkg/depcache.h>
#include <apt-pkg/pkgcache.h>
#include <apt-pkg/algorithms.h>
#include <apt-pkg/error.h>
#include <apt-pkg/version.h>
#include <apt-pkg/pkgsystem.h>
#include <apt-pkg/sourcelist.h>
#include <apt-pkg/pkgrecords.h>
#include <apt-pkg/acquire.h>
#include <apt-pkg/packagemanager.h>
#include <apt-pkg/dpkgpm.h>
#include <apt-pkg/install-progress.h>

#include <algorithm>
#include <fnmatch.h>
#include <map>
#include <set>
#include <sstream>

struct Skip {
   std::string name, oldver, newver, reason;
};

struct AptSession {
   pkgCacheFile *CF = nullptr;
   pkgRecords *Recs = nullptr;
   bool lock = false;
   std::map<std::string, std::string> localByPkg;
   std::vector<Skip> skipped;
};

namespace {

bool gInit = false;

bool ensureInit() {
   if (gInit)
      return true;
   _config = new Configuration();
   if (!pkgInitConfig(*_config))
      return false;
   _system = nullptr;
   if (!pkgInitSystem(*_config, _system))
      return false;
   _config->Set("APT::Install-Recommends", "false");
   _config->Set("APT::Install-Suggests", "false");
   _config->Set("APT::Get::Always-Include-Phased-Updates", "true");
   _config->Set("APT::Get::Phase-Policy", "true");
   gInit = true;
   return true;
}

std::string jsonEscape(const std::string &s) {
   std::ostringstream out;
   out << '"';
   for (unsigned char c : s) {
      switch (c) {
      case '"': out << "\\\""; break;
      case '\\': out << "\\\\"; break;
      case '\n': out << "\\n"; break;
      case '\r': out << "\\r"; break;
      case '\t': out << "\\t"; break;
      default:
         if (c < 0x20) {
            char buf[7];
            std::snprintf(buf, sizeof(buf), "\\u%04x", c);
            out << buf;
         } else {
            out << c;
         }
      }
   }
   out << '"';
   return out.str();
}

std::string errorsToString() {
   std::ostringstream out;
   _error->DumpErrors(out);
   std::string s = out.str();
   while (!s.empty() && (s.back() == '\n' || s.back() == ' '))
      s.pop_back();
   return s;
}

bool hasRepoFile(pkgCache::VerIterator V) {
   for (pkgCache::VerFileIterator F = V.FileList(); !F.end(); ++F)
      if (!F.File().Flagged(pkgCache::Flag::NotSource))
         return true;
   return false;
}

bool versionInRepo(pkgCache::VerIterator V, const std::string &repo) {
   if (repo.empty())
      return true;
   for (pkgCache::VerFileIterator F = V.FileList(); !F.end(); ++F) {
      pkgCache::PkgFileIterator Pf = F.File();
      if (Pf.Flagged(pkgCache::Flag::LocalSource))
         return repo == "local";
      const char *comp = Pf.Component();
      const char *site = Pf.Site();
      const char *label = Pf.Label();
      pkgCache::RlsFileIterator rf = Pf.ReleaseFile();
      const char *codename = rf.end() ? nullptr : rf.Codename();
      if (codename != nullptr && comp != nullptr &&
          repo == std::string(codename) + "/" + comp)
         return true;
      if (comp != nullptr && repo == comp)
         return true;
      if (site != nullptr && repo == site)
         return true;
      if (label != nullptr && repo == label)
         return true;
      if (comp != nullptr && repo.size() > strlen(comp) &&
          repo.compare(repo.size() - strlen(comp), strlen(comp), comp) == 0)
         return true;
   }
   return false;
}

pkgCache::VerIterator maxVersion(pkgCache::PkgIterator P) {
   pkgCache::VerIterator best;
   for (pkgCache::VerIterator V = P.VersionList(); !V.end(); ++V) {
      if (!hasRepoFile(V))
         continue;
      if (best.end() || _system->VS->CmpVersion(best.VerStr(), V.VerStr()) < 0)
         best = V;
   }
   return best;
}

pkgCache::VerIterator selectVersion(pkgCache::PkgIterator P, const char *version, const char *repo) {
   std::string ver = version == nullptr ? "" : version;
   std::string rel = repo == nullptr ? "" : repo;
   pkgCache::VerIterator best;
   for (pkgCache::VerIterator V = P.VersionList(); !V.end(); ++V) {
      if (!ver.empty() && ver != V.VerStr())
         continue;
      if (!versionInRepo(V, rel))
         continue;
      if (best.end() || _system->VS->CmpVersion(best.VerStr(), V.VerStr()) < 0)
         best = V;
   }
   return best;
}

pkgCache::PkgIterator findPkg(pkgCache &C, const std::string &name) {
   std::string n = name;
   std::string arch;
   auto pos = n.find(':');
   if (pos != std::string::npos) {
      arch = n.substr(pos + 1);
      n = n.substr(0, pos);
   }
   if (!arch.empty()) {
      pkgCache::PkgIterator P = C.FindPkg(n, arch);
      if (!P.end())
         return P;
   } else {
      std::string native = _config->Find("APT::Architecture");
      if (!native.empty()) {
         pkgCache::PkgIterator P = C.FindPkg(n, native);
         if (!P.end()) {
            for (pkgCache::VerIterator V = P.VersionList(); !V.end(); ++V)
               if (hasRepoFile(V))
                  return P;
            if (!P.CurrentVer().end())
               return P;
         }
      }
   }
   pkgCache::PkgIterator P = C.FindPkg(n, "any");
   if (P.end())
      P = C.FindPkg(n);
   return P;
}

void selectAllCandidates(pkgCache &C, pkgDepCache &D) {
   for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P) {
      pkgCache::VerIterator best = maxVersion(P);
      if (!best.end())
         D.SetCandidateVersion(best);
   }
}

std::string repoLabel(pkgCache::VerIterator V) {
   for (pkgCache::VerFileIterator F = V.FileList(); !F.end(); ++F) {
      pkgCache::PkgFileIterator pf = F.File();
      if (pf.Flagged(pkgCache::Flag::NotSource))
         continue;
      if (pf.Flagged(pkgCache::Flag::LocalSource))
         return "local";
      pkgCache::RlsFileIterator rf = pf.ReleaseFile();
      std::string suite;
      if (!rf.end()) {
         if (rf.Codename() != nullptr)
            suite = rf.Codename();
         else if (rf.Archive() != nullptr)
            suite = rf.Archive();
      }
      const char *comp = pf.Component();
      if (!suite.empty() && comp != nullptr)
         return suite + "/" + comp;
      if (comp != nullptr)
         return comp;
      if (pf.Site() != nullptr)
         return pf.Site();
   }
   return "";
}

void collectProblems(pkgCache &C, pkgDepCache &D, std::vector<std::string> &out) {
   for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P) {
      if (!D[P].InstBroken())
         continue;
      pkgCache::VerIterator V = D[P].InstVerIter(C);
      if (V.end())
         continue;
      for (pkgCache::DepIterator Dep = V.DependsList(); !Dep.end();) {
         pkgCache::DepIterator Start;
         pkgCache::DepIterator End;
         Dep.GlobOr(Start, End);
         if (!D.IsImportantDep(End))
            continue;
         if ((D[End] & pkgDepCache::DepGInstall) == pkgDepCache::DepGInstall)
            continue;
         std::ostringstream s;
         s << P.FullName(true) << ": " << End.DepType() << ": "
           << Start.TargetPkg().FullName(true);
         if (Start.TargetVer() != nullptr)
            s << " (" << Start.CompType() << " " << Start.TargetVer() << ")";
         pkgCache::VerIterator T = D[Start.TargetPkg()].InstVerIter(C);
         if (T.end())
            s << " but it is not going to be installed";
         else
            s << " but " << T.VerStr() << " is to be installed";
         out.push_back(s.str());
      }
   }
}

std::string verStr(pkgCache::VerIterator V) {
   return V.end() ? std::string() : std::string(V.VerStr());
}

std::string actionName(pkgCache &C, pkgDepCache &D, pkgCache::PkgIterator P) {
   if (D[P].Delete())
      return D[P].Purge() ? "purge" : "remove";
   if (D[P].ReInstall())
      return "reinstall";
   if (P.CurrentVer().end())
      return "install";
   pkgCache::VerIterator I = D[P].InstVerIter(C);
   if (I.end())
      return "keep";
   int c = _system->VS->CmpVersion(I.VerStr(), P.CurrentVer().VerStr());
   if (c < 0)
      return "downgrade";
   if (c > 0)
      return "upgrade";
   return "reinstall";
}

struct Action {
   std::string name, arch, oldver, newver, kind, repo, site, filename, deb;
   std::string reason, other, otherrepo;
   unsigned long long download = 0;
   unsigned long long installed = 0;
   bool explicitPkg = false;
   bool purge = false;
};

std::string actionJSON(const Action &a) {
   std::ostringstream o;
   o << "{";
   o << "\"name\":" << jsonEscape(a.name);
   o << ",\"arch\":" << jsonEscape(a.arch);
   o << ",\"old\":" << jsonEscape(a.oldver);
   o << ",\"new\":" << jsonEscape(a.newver);
   o << ",\"kind\":" << jsonEscape(a.kind);
   o << ",\"repo\":" << jsonEscape(a.repo);
   o << ",\"site\":" << jsonEscape(a.site);
   o << ",\"filename\":" << jsonEscape(a.filename);
   o << ",\"deb\":" << jsonEscape(a.deb);
   o << ",\"reason\":" << jsonEscape(a.reason);
   o << ",\"other\":" << jsonEscape(a.other);
   o << ",\"otherrepo\":" << jsonEscape(a.otherrepo);
   o << ",\"download\":" << a.download;
   o << ",\"installed\":" << a.installed;
   o << ",\"explicit\":" << (a.explicitPkg ? "true" : "false");
   o << ",\"purge\":" << (a.purge ? "true" : "false");
   o << "}";
   return o.str();
}

std::string makeResponse(const std::vector<Action> &actions,
                         const std::vector<std::string> &problems,
                         const std::string &error,
                         const std::vector<Skip> &skipped = {}) {
   std::ostringstream o;
   o << "{\"error\":" << jsonEscape(error) << ",\"problems\":[";
   for (size_t i = 0; i < problems.size(); ++i) {
      if (i)
         o << ",";
      o << jsonEscape(problems[i]);
   }
   o << "],\"actions\":[";
   for (size_t i = 0; i < actions.size(); ++i) {
      if (i)
         o << ",";
      o << actionJSON(actions[i]);
   }
   o << "],\"skipped\":[";
   for (size_t i = 0; i < skipped.size(); ++i) {
      if (i)
         o << ",";
      o << "{\"name\":" << jsonEscape(skipped[i].name)
        << ",\"old\":" << jsonEscape(skipped[i].oldver)
        << ",\"new\":" << jsonEscape(skipped[i].newver)
        << ",\"reason\":" << jsonEscape(skipped[i].reason) << "}";
   }
   o << "]}";
   return o.str();
}

char *dupJSON(const std::string &s) {
   char *p = static_cast<char *>(std::malloc(s.size() + 1));
   if (p == nullptr)
      return nullptr;
   std::memcpy(p, s.c_str(), s.size() + 1);
   return p;
}

bool openCache(AptSession *s, const AptRequest *req) {
   s->CF = new pkgCacheFile();
   bool lock = (req->flags & APT_FLAG_LOCK) != 0;
   _config->Set("APT::Ignore-Hold", (req->flags & APT_FLAG_OVERRIDE_HELD) != 0 ? "true" : "false");
   if (req->arch != nullptr && req->arch[0] != '\0')
      _config->Set("APT::Architecture", req->arch);
   if (req->narchitectures > 0) {
      _config->Clear("APT::Architectures");
      for (int i = 0; i < req->narchitectures; ++i)
         if (req->architectures[i] != nullptr && req->architectures[i][0] != '\0')
            _config->Set("APT::Architectures::", req->architectures[i]);
   }
   if (req->cachedir != nullptr && req->cachedir[0] != '\0')
      _config->Set("Dir::Cache::archives", req->cachedir);
   if (!s->CF->BuildSourceList())
      return false;
   for (int i = 0; i < req->ntargets; ++i) {
      const char *deb = req->debs == nullptr ? nullptr : req->debs[i];
      if (deb == nullptr || deb[0] == '\0')
         continue;
      if (!s->CF->GetSourceList()->AddVolatileFile(deb, nullptr))
         return false;
      const char *name = req->names[i];
      if (name != nullptr) {
         s->localByPkg[name] = deb;
         std::string bare = name;
         auto pos = bare.find(':');
         if (pos != std::string::npos)
            s->localByPkg[bare.substr(0, pos)] = deb;
      }
   }
   s->lock = lock;
   if (!s->CF->BuildCaches(nullptr, lock))
      return false;
   if (!s->CF->BuildPolicy())
      return false;
   if (!s->CF->BuildDepCache())
      return false;
   s->Recs = new pkgRecords(*s->CF);
   return true;
}

bool doInstall(AptSession *s, const AptRequest *req, std::vector<std::string> &problems) {
   pkgCache &C = *s->CF->GetPkgCache();
   pkgDepCache &D = *s->CF->GetDepCache();
   selectAllCandidates(C, D);
   bool needed = (req->flags & APT_FLAG_NEEDED) != 0;
   bool upgradeOnly = (req->flags & APT_FLAG_UPGRADE_ONLY) != 0;
   bool asDeps = (req->flags & APT_FLAG_ASDEPS) != 0;
   bool asExplicit = (req->flags & APT_FLAG_ASEXPLICIT) != 0;
   pkgProblemResolver Fix(&D);
   std::vector<pkgCache::PkgIterator> marked;
   for (int i = 0; i < req->ntargets; ++i) {
      const char *name = req->names[i];
      if (name == nullptr || name[0] == '\0')
         continue;
      pkgCache::PkgIterator P = findPkg(C, name);
      if (P.end()) {
         problems.push_back(std::string("target not found: ") + name);
         return false;
      }
      if (upgradeOnly && P.CurrentVer().end())
         continue;
      pkgCache::VerIterator V = selectVersion(P, req->versions ? req->versions[i] : nullptr,
                                              req->repos ? req->repos[i] : nullptr);
      if (V.end()) {
         std::string what = name;
         if (req->versions && req->versions[i] && req->versions[i][0])
            what += std::string("=") + req->versions[i];
         problems.push_back("version not found: " + what);
         return false;
      }
      D.SetCandidateVersion(V);
      if (!P.CurrentVer().end() &&
          _system->VS->CmpVersion(V.VerStr(), P.CurrentVer().VerStr()) == 0) {
         if (needed) {
            s->skipped.push_back({P.Name(), verStr(P.CurrentVer()), verStr(V), "needed"});
            continue;
         }
         D.SetReInstall(P, true);
      }
      Fix.Clear(P);
      Fix.Protect(P);
      if (!D.MarkInstall(P, false)) {
         problems.push_back(std::string("could not mark ") + name + " for install");
         return false;
      }
      marked.push_back(P);
   }
   for (auto P : marked)
      D.MarkInstall(P, true);
   if (!Fix.Resolve(true) || D.BrokenCount() != 0) {
      collectProblems(C, D, problems);
      if (problems.empty())
         problems.push_back("could not resolve dependencies");
      return false;
   }
   if (asDeps)
      for (auto P : marked)
         D.MarkAuto(P, true);
   if (asExplicit)
      for (auto P : marked)
         D.MarkAuto(P, false);
   return true;
}

bool matchesPattern(const char *value, const char **patterns, int count) {
   if (value == nullptr)
      return false;
   for (int i = 0; i < count; ++i)
      if (patterns[i] != nullptr && fnmatch(patterns[i], value, 0) == 0)
         return true;
   return false;
}

bool isExplicitTarget(const std::string &name, const AptRequest *req) {
   for (int i = 0; i < req->ntargets; ++i) {
      const char *target = req->names == nullptr ? nullptr : req->names[i];
      if (target == nullptr)
         continue;
      if (name == target)
         return true;
      std::string bare = target;
      auto pos = bare.find(':');
      if (pos != std::string::npos && bare.substr(0, pos) == name)
         return true;
   }
   return false;
}

bool doUpgrade(AptSession *s, const AptRequest *req, std::vector<std::string> &problems) {
   pkgCache &C = *s->CF->GetPkgCache();
   pkgDepCache &D = *s->CF->GetDepCache();
   selectAllCandidates(C, D);
   bool allowDowngrade = (req->flags & APT_FLAG_DOWNGRADE) != 0;
   bool needed = (req->flags & APT_FLAG_NEEDED) != 0;
   bool upgradeOnly = (req->flags & APT_FLAG_UPGRADE_ONLY) != 0;
   pkgProblemResolver Fix(&D);
   std::vector<pkgCache::PkgIterator> marked;

   // Explicit targets are handled first so -Su pkg behaves like pacman.
   for (int i = 0; i < req->ntargets; ++i) {
      const char *name = req->names == nullptr ? nullptr : req->names[i];
      if (name == nullptr || name[0] == '\0')
         continue;
      pkgCache::PkgIterator P = findPkg(C, name);
      if (P.end()) {
         problems.push_back(std::string("target not found: ") + name);
         return false;
      }
      if (upgradeOnly && P.CurrentVer().end())
         continue;
      pkgCache::VerIterator V = selectVersion(P, req->versions ? req->versions[i] : nullptr,
                                              req->repos ? req->repos[i] : nullptr);
      if (V.end()) {
         std::string what = name;
         if (req->versions && req->versions[i] && req->versions[i][0])
            what += std::string("=") + req->versions[i];
         problems.push_back("version not found: " + what);
         return false;
      }
      D.SetCandidateVersion(V);
      if (!P.CurrentVer().end() &&
          _system->VS->CmpVersion(V.VerStr(), P.CurrentVer().VerStr()) == 0) {
         if (needed) {
            s->skipped.push_back({P.Name(), verStr(P.CurrentVer()), verStr(V), "needed"});
            continue;
         }
         D.SetReInstall(P, true);
      }
      Fix.Clear(P);
      Fix.Protect(P);
      if (!D.MarkInstall(P, false)) {
         problems.push_back(std::string("could not mark ") + name + " for install");
         return false;
      }
      marked.push_back(P);
   }

   for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P) {
      if (P.CurrentVer().end())
         continue;
      std::string name = P.Name();
      pkgCache::VerIterator V = D.GetCandidateVersion(P);
      if (P->SelectedState == pkgCache::State::Hold) {
         if (!isExplicitTarget(name, req))
            s->skipped.push_back({name, verStr(P.CurrentVer()), verStr(V), "held"});
         continue;
      }
      if (V.end())
         continue;
      if (!isExplicitTarget(name, req)) {
         if (matchesPattern(name.c_str(), req->ignorepkgs, req->nignorepkgs)) {
            s->skipped.push_back({name, verStr(P.CurrentVer()), verStr(V), "ignored"});
            continue;
         }
         if (matchesPattern(V.Section(), req->ignoregroups, req->nignoregroups)) {
            s->skipped.push_back({name, verStr(P.CurrentVer()), verStr(V), "ignored"});
            continue;
         }
      }
      int c = _system->VS->CmpVersion(V.VerStr(), P.CurrentVer().VerStr());
      if (c > 0 || (allowDowngrade && c < 0)) {
         Fix.Clear(P);
         Fix.Protect(P);
         D.MarkInstall(P, false);
         marked.push_back(P);
      }
   }
   for (auto P : marked)
      D.MarkInstall(P, true);
   if (!Fix.Resolve(true) || D.BrokenCount() != 0) {
      collectProblems(C, D, problems);
      if (problems.empty())
         problems.push_back("could not resolve dependencies");
      return false;
   }
   for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P)
      if (D[P].Delete())
         problems.push_back(std::string("upgrade would remove ") + P.Name());
   return problems.empty();
}

bool doRemove(AptSession *s, const AptRequest *req, std::vector<std::string> &problems) {
   pkgCache &C = *s->CF->GetPkgCache();
   pkgDepCache &D = *s->CF->GetDepCache();
   bool purge = (req->flags & APT_FLAG_PURGE) != 0;
   bool recursive = (req->flags & APT_FLAG_RECURSIVE) != 0;
   bool recursiveAll = (req->flags & APT_FLAG_RECURSIVE_ALL) != 0;
   bool cascade = (req->flags & APT_FLAG_CASCADE) != 0;
   bool forceDeps = (req->flags & APT_FLAG_FORCE_DEPS) != 0;
   std::map<std::string, bool> baseline;
   for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P)
      if (D[P].InstBroken())
         baseline[P.Name()] = true;

   for (int i = 0; i < req->ntargets; ++i) {
      const char *name = req->names[i];
      if (name == nullptr || name[0] == '\0')
         continue;
      pkgCache::PkgIterator P = findPkg(C, name);
      if (P.end() || P.CurrentVer().end()) {
         problems.push_back(std::string("target not found: ") + name);
         return false;
      }
      if (!D.MarkDelete(P, purge)) {
         problems.push_back(std::string("could not mark ") + name + " for removal");
         return false;
      }
   }
   if (recursive) {
      std::set<std::string> closure;
      std::vector<pkgCache::PkgIterator> queue;
      for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P)
         if (D[P].Delete() && !P.CurrentVer().end())
            queue.push_back(P);
      for (size_t head = 0; head < queue.size(); ++head) {
         pkgCache::PkgIterator P = queue[head];
         pkgCache::VerIterator V = P.CurrentVer();
         if (V.end())
            continue;
         for (pkgCache::DepIterator Dep = V.DependsList(); !Dep.end(); ++Dep) {
            if (Dep->Type != pkgCache::Dep::Depends && Dep->Type != pkgCache::Dep::PreDepends)
               continue;
            pkgCache::PkgIterator T = Dep.TargetPkg();
            std::vector<pkgCache::PkgIterator> providers;
            if (!T.end()) {
               providers.push_back(T);
               for (pkgCache::PrvIterator Prv = T.ProvidesList(); !Prv.end(); ++Prv)
                  providers.push_back(Prv.OwnerPkg());
            }
            for (auto Q : providers) {
               if (Q.end() || Q.CurrentVer().end() || closure.count(Q.Name()) != 0)
                  continue;
               closure.insert(Q.Name());
               queue.push_back(Q);
            }
         }
      }
      D.MarkAndSweep();
      std::set<std::string> removalSet;
      for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P)
         if (D[P].Delete())
            removalSet.insert(P.Name());
      for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P)
         if (D[P].Garbage && closure.count(P.Name()) != 0 && !D[P].Delete()) {
            D.MarkDelete(P, purge);
            removalSet.insert(P.Name());
         }
      if (recursiveAll) {
         for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P) {
            if (P.CurrentVer().end() || D[P].Delete() || closure.count(P.Name()) == 0)
               continue;
            D.MarkDelete(P, purge);
            bool needed = false;
            for (pkgCache::PkgIterator Q = C.PkgBegin(); !Q.end(); ++Q) {
               if (Q.CurrentVer().end() || removalSet.count(Q.Name()) != 0)
                  continue;
               if (D[Q].InstBroken()) {
                  needed = true;
                  break;
               }
            }
            if (needed)
               D.MarkKeep(P, false, true);
            else
               removalSet.insert(P.Name());
         }
      }
   }
   if (cascade) {
      for (int round = 0; round < 100; ++round) {
         bool changed = false;
         for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P) {
            if (P.CurrentVer().end() || D[P].Delete())
               continue;
            if (baseline.find(P.Name()) != baseline.end())
               continue;
            if (D[P].InstBroken()) {
               D.MarkDelete(P, purge);
               changed = true;
            }
         }
         if (!changed)
            break;
      }
   }
   if (!forceDeps && D.BrokenCount() != 0) {
      collectProblems(C, D, problems);
      if (problems.empty())
         problems.push_back("could not resolve dependencies");
      return false;
   }
   return true;
}

bool depMatches(pkgCache::DepIterator Dep, pkgCache::PkgIterator P) {
   if (P.CurrentVer().end())
      return false;
   if (Dep.IsSatisfied(P.CurrentVer()))
      return true;
   for (pkgCache::PrvIterator Prv = P.CurrentVer().ProvidesList(); !Prv.end(); ++Prv)
      if (Dep.IsSatisfied(Prv))
         return true;
   return false;
}

void removalReason(pkgCache &C, pkgDepCache &D, pkgCache::PkgIterator P, Action &a) {
   for (pkgCache::PkgIterator Q = C.PkgBegin(); !Q.end(); ++Q) {
      if (!D[Q].Install())
         continue;
      pkgCache::VerIterator QV = D[Q].InstVerIter(C);
      if (QV.end())
         continue;
      for (pkgCache::DepIterator Dep = QV.DependsList(); !Dep.end(); ++Dep) {
         if (Dep->Type != pkgCache::Dep::Conflicts && Dep->Type != pkgCache::Dep::DpkgBreaks)
            continue;
         if (!depMatches(Dep, P))
            continue;
         a.reason = "conflict";
         a.other = Q.Name();
         a.otherrepo = repoLabel(QV);
         return;
      }
   }
   for (pkgCache::PkgIterator Q = C.PkgBegin(); !Q.end(); ++Q) {
      if (!D[Q].Install())
         continue;
      pkgCache::VerIterator QV = D[Q].InstVerIter(C);
      if (QV.end())
         continue;
      for (pkgCache::DepIterator Dep = QV.DependsList(); !Dep.end(); ++Dep) {
         if (Dep->Type != pkgCache::Dep::Replaces)
            continue;
         if (!depMatches(Dep, P))
            continue;
         a.reason = "replace";
         a.other = Q.Name();
         a.otherrepo = repoLabel(QV);
         return;
      }
   }
}

std::vector<Action> collectActions(AptSession *s) {
   pkgCache &C = *s->CF->GetPkgCache();
   pkgDepCache &D = *s->CF->GetDepCache();
   std::vector<Action> out;
   for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P) {
      if (!D[P].Install() && !D[P].Delete() && !D[P].ReInstall())
         continue;
      Action a;
      a.name = P.Name();
      a.arch = P.Arch() == nullptr ? "" : P.Arch();
      a.oldver = verStr(P.CurrentVer());
      a.kind = actionName(C, D, P);
      if (a.kind == "keep")
         continue;
      a.explicitPkg = !(D[P].Flags & pkgCache::Flag::Auto);
      a.purge = D[P].Purge();
      pkgCache::VerIterator I = D[P].InstVerIter(C);
      if (!I.end()) {
         a.newver = verStr(I);
         a.download = I->Size;
         a.installed = I->InstalledSize;
         a.repo = repoLabel(I);
         a.filename = s->Recs->Lookup(I.FileList()).FileName();
         for (pkgCache::VerFileIterator F = I.FileList(); !F.end(); ++F) {
            if (F.File().Flagged(pkgCache::Flag::NotSource))
               continue;
            if (F.File().Site() != nullptr) {
               a.site = F.File().Site();
               break;
            }
         }
      } else if (!P.CurrentVer().end()) {
         a.installed = P.CurrentVer()->InstalledSize;
         a.repo = repoLabel(P.CurrentVer());
      }
      auto it = s->localByPkg.find(a.name);
      if (it != s->localByPkg.end())
         a.deb = it->second;
      if (a.kind == "remove" || a.kind == "purge") {
         a.name = P.FullName(true);
         removalReason(C, D, P, a);
      }
      out.push_back(a);
   }
   return out;
}

bool versionOK(const char *have, const char *op, const char *want) {
   if (op == nullptr || op[0] == '\0')
      return true;
   if (have == nullptr)
      return false;
   int c = _system->VS->CmpVersion(have, want == nullptr ? "" : want);
   std::string o = op;
   if (o == "=")
      return c == 0;
   if (o == ">=")
      return c >= 0;
   if (o == "<=")
      return c <= 0;
   if (o == ">>" || o == ">")
      return c > 0;
   if (o == "<<" || o == "<")
      return c < 0;
   return false;
}

bool installedSatisfies(pkgCache &C, const std::string &name, const char *op, const char *want) {
   pkgCache::PkgIterator P = findPkg(C, name);
   if (P.end())
      return false;
   pkgCache::VerIterator V = P.CurrentVer();
   if (!V.end() && versionOK(V.VerStr(), op, want))
      return true;
   for (pkgCache::PrvIterator Prv = P.ProvidesList(); !Prv.end(); ++Prv) {
      pkgCache::VerIterator OV = Prv.OwnerVer();
      if (OV.end() || OV.ParentPkg().CurrentVer() != OV)
         continue;
      const char *pv = Prv.ProvideVersion();
      const char *have = (pv != nullptr && pv[0] != '\0') ? pv : OV.VerStr();
      if (versionOK(have, op, want))
         return true;
   }
   return false;
}

std::string depGroupString(pkgCache::DepIterator Start, pkgCache::DepIterator End) {
   std::string out;
   for (pkgCache::DepIterator D = Start;; ++D) {
      if (!out.empty())
         out += "|";
      out += D.TargetPkg().Name();
      if (D.TargetVer() != nullptr)
         out += std::string(D.CompType()) + D.TargetVer();
      if (D == End)
         break;
   }
   return out;
}

std::string listJSON(const char *key, const std::vector<std::string> &items) {
   std::ostringstream o;
   o << "{\"" << key << "\":[";
   for (size_t i = 0; i < items.size(); ++i) {
      if (i)
         o << ",";
      o << jsonEscape(items[i]);
   }
   o << "]}";
   return o.str();
}

struct ManualRootSet : pkgDepCache::InRootSetFunc {
   pkgDepCache &D;
   explicit ManualRootSet(pkgDepCache &d) : D(d) {}
   bool InRootSet(const pkgCache::PkgIterator &P) override {
      return P.end() == false && P.CurrentVer().end() == false &&
             (D[P].Flags & pkgCache::Flag::Auto) == 0;
   }
};

} // namespace

extern "C" {

char *aptplan_resolve(const AptRequest *req, AptSession **session) {
   if (session != nullptr)
      *session = nullptr;
   if (req == nullptr)
      return dupJSON(makeResponse({}, {}, "invalid request"));
   if (!ensureInit())
      return dupJSON(makeResponse({}, {}, "could not initialize libapt"));
   _error->Discard();

   AptSession *s = new AptSession();
   std::vector<std::string> problems;
   bool ok = openCache(s, req);
   if (ok) {
      switch (req->op) {
      case APT_OP_INSTALL: ok = doInstall(s, req, problems); break;
      case APT_OP_REMOVE: ok = doRemove(s, req, problems); break;
      case APT_OP_UPGRADE: ok = doUpgrade(s, req, problems); break;
      default: problems.push_back("unknown operation"); ok = false; break;
      }
   } else {
      problems.push_back("could not open the apt cache");
   }
   std::string err;
   if (!ok && _error->PendingError())
      err = errorsToString();
   std::vector<Action> actions;
   if (ok)
      actions = collectActions(s);
   char *json = dupJSON(makeResponse(actions, problems, err, s->skipped));
   if (!ok || session == nullptr) {
      aptplan_close(s);
   } else {
      *session = s;
   }
   return json;
}

char *aptplan_execute(AptSession *s, int download_only) {
   if (s == nullptr || s->CF == nullptr)
      return dupJSON(makeResponse({}, {}, "invalid session"));
   _error->Discard();
   pkgDepCache &D = *s->CF->GetDepCache();
   std::string err;
   pkgPackageManager *PM = _system->CreatePM(&D);
   if (PM == nullptr) {
      err = "could not create the package manager";
   } else {
      pkgAcquire Fetcher(nullptr);
      if (!Fetcher.GetLock(_config->FindDir("Dir::Cache::Archives"))) {
         err = "could not lock the archive cache";
      } else if (!PM->GetArchives(&Fetcher, s->CF->GetSourceList(), s->Recs)) {
         err = errorsToString();
         if (err.empty())
            err = "could not prepare the download";
      } else {
         pkgAcquire::RunResult res = Fetcher.Run();
         if (res != pkgAcquire::Continue) {
            err = errorsToString();
            if (err.empty())
               err = "download failed";
         } else if (!download_only) {
            Fetcher.Shutdown();
            _system->UnLockInner();
            auto *progress = APT::Progress::PackageManagerProgressFactory();
            pkgPackageManager::OrderResult r = PM->DoInstall(progress);
            delete progress;
            _system->LockInner();
            D.writeStateFile(nullptr, true);
            if (r != pkgPackageManager::Completed) {
               err = errorsToString();
               if (err.empty())
                  err = "installation failed";
            }
         }
      }
   }
   delete PM;
   return dupJSON(makeResponse({}, {}, err));
}

void aptplan_close(AptSession *s) {
   if (s == nullptr)
      return;
   delete s->Recs;
   delete s->CF;
   delete s;
}

char *aptplan_nativearch(void) {
   if (!ensureInit())
      return dupJSON("{\"error\":\"could not initialize libapt\"}");
   std::ostringstream o;
   o << "{\"arch\":" << jsonEscape(_config->Find("APT::Architecture")) << "}";
   return dupJSON(o.str());
}

void aptplan_configure(const char *arch, const char *cachedir,
                       const char **architectures, int narchitectures) {
   if (!ensureInit())
      return;
   if (arch != nullptr && arch[0] != '\0')
      _config->Set("APT::Architecture", arch);
   if (narchitectures > 0) {
      _config->Clear("APT::Architectures");
      for (int i = 0; i < narchitectures; ++i)
         if (architectures != nullptr && architectures[i] != nullptr && architectures[i][0] != '\0')
            _config->Set("APT::Architectures::", architectures[i]);
   }
   if (cachedir != nullptr && cachedir[0] != '\0')
      _config->Set("Dir::Cache::archives", cachedir);
}

char *aptplan_unneeded(const AptDepQuery *q) {
   if (q == nullptr)
      return dupJSON("{\"error\":\"invalid query\"}");
   if (!ensureInit())
      return dupJSON("{\"error\":\"could not initialize libapt\"}");
   _error->Discard();
   pkgCacheFile CF;
   if (!CF.ReadOnlyOpen())
      return dupJSON("{\"error\":" + jsonEscape(errorsToString()) + "}");
   pkgCache &C = *CF.GetPkgCache();
   pkgDepCache &D = *CF.GetDepCache();
   std::set<std::string> targets;
   for (int i = 0; i < q->ntargets; ++i)
      if (q->names != nullptr && q->names[i] != nullptr)
         targets.insert(q->names[i]);
   std::set<std::string> baseline;
   for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P)
      if (D[P].InstBroken())
         baseline.insert(P.Name());
   std::vector<std::string> out;
   for (int i = 0; i < q->ntargets; ++i) {
      const char *name = q->names == nullptr ? nullptr : q->names[i];
      if (name == nullptr || name[0] == '\0')
         continue;
      pkgCache::PkgIterator P = findPkg(C, name);
      if (P.end() || P.CurrentVer().end())
         continue;
      D.MarkDelete(P, false, true);
      bool needed = false;
      for (pkgCache::PkgIterator Q = C.PkgBegin(); !Q.end(); ++Q) {
         if (Q.CurrentVer().end() || targets.count(Q.Name()) != 0)
            continue;
         if (baseline.count(Q.Name()) != 0)
            continue;
         if (D[Q].InstBroken()) {
            needed = true;
            break;
         }
      }
      D.MarkKeep(P, false, true);
      if (!needed)
         out.push_back(name);
   }
   return dupJSON(listJSON("unneeded", out));
}

char *aptplan_policy(const AptDepQuery *q) {
   if (q == nullptr)
      return dupJSON("{\"error\":\"invalid query\"}");
   if (!ensureInit())
      return dupJSON("{\"error\":\"could not initialize libapt\"}");
   _error->Discard();
   pkgCacheFile CF;
   if (!CF.ReadOnlyOpen())
      return dupJSON("{\"error\":" + jsonEscape(errorsToString()) + "}");
   pkgCache &C = *CF.GetPkgCache();
   pkgDepCache &D = *CF.GetDepCache();
   selectAllCandidates(C, D);
   std::ostringstream o;
   o << "{\"packages\":[";
   for (int i = 0; i < q->ntargets; ++i) {
      if (i)
         o << ",";
      const char *name = q->names == nullptr ? nullptr : q->names[i];
      pkgCache::PkgIterator P = name == nullptr ? pkgCache::PkgIterator(C) : findPkg(C, name);
      std::string installed;
      std::string candidate;
      std::string repo;
      bool fromRepo = false;
      if (!P.end()) {
         installed = verStr(P.CurrentVer());
         pkgCache::VerIterator V = D.GetCandidateVersion(P);
         candidate = verStr(V);
         if (!V.end() && hasRepoFile(V)) {
            fromRepo = true;
            repo = repoLabel(V);
         }
      }
      o << "{\"name\":" << jsonEscape(name == nullptr ? "" : name)
        << ",\"installed\":" << jsonEscape(installed)
        << ",\"candidate\":" << jsonEscape(candidate)
        << ",\"repo\":" << jsonEscape(repo)
        << ",\"fromrepo\":" << (fromRepo ? "true" : "false")
        << ",\"found\":" << (P.end() ? "false" : "true") << "}";
   }
   o << "]}";
   return dupJSON(o.str());
}

char *aptplan_checkdeps(const AptDepQuery *q) {
   if (q == nullptr)
      return dupJSON("{\"error\":\"invalid query\"}");
   if (!ensureInit())
      return dupJSON("{\"error\":\"could not initialize libapt\"}");
   _error->Discard();
   pkgCacheFile CF;
   if (!CF.ReadOnlyOpen())
      return dupJSON("{\"error\":" + jsonEscape(errorsToString()) + "}");
   pkgCache &C = *CF.GetPkgCache();
   std::vector<std::string> missing;
   for (int i = 0; i < q->ntargets; ++i) {
      const char *name = q->names == nullptr ? nullptr : q->names[i];
      if (name == nullptr || name[0] == '\0')
         continue;
      const char *op = q->ops == nullptr ? nullptr : q->ops[i];
      const char *ver = q->versions == nullptr ? nullptr : q->versions[i];
      if (installedSatisfies(C, name, op, ver))
         continue;
      std::string spec = name;
      if (op != nullptr)
         spec += op;
      if (ver != nullptr)
         spec += ver;
      missing.push_back(spec);
   }
   return dupJSON(listJSON("missing", missing));
}

char *aptplan_checkdb(void) {
   if (!ensureInit())
      return dupJSON("{\"error\":\"could not initialize libapt\"}");
   _error->Discard();
   pkgCacheFile CF;
   if (!CF.ReadOnlyOpen())
      return dupJSON("{\"error\":" + jsonEscape(errorsToString()) + "}");
   pkgCache &C = *CF.GetPkgCache();
   pkgDepCache &D = *CF.GetDepCache();
   std::vector<std::string> problems;
   for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P) {
      pkgCache::VerIterator V = P.CurrentVer();
      if (V.end() || !D[P].NowBroken())
         continue;
      for (pkgCache::DepIterator Dep = V.DependsList(); !Dep.end();) {
         pkgCache::DepIterator Start;
         pkgCache::DepIterator End;
         Dep.GlobOr(Start, End);
         if (End->Type != pkgCache::Dep::Depends && End->Type != pkgCache::Dep::PreDepends)
            continue;
         if ((D[End] & pkgDepCache::DepGNow) == pkgDepCache::DepGNow)
            continue;
         problems.push_back("missing '" + depGroupString(Start, End) +
                            "' dependency for '" + P.Name() + "'");
      }
   }
   return dupJSON(listJSON("problems", problems));
}

char *aptplan_orphans(int ignore_optdeps) {
   if (!ensureInit())
      return dupJSON("{\"error\":\"could not initialize libapt\"}");
   _error->Discard();
   _config->Set("APT::Install-Recommends", ignore_optdeps ? "false" : "true");
   _config->Set("APT::Install-Suggests", ignore_optdeps ? "false" : "true");
   pkgCacheFile CF;
   bool ok = CF.ReadOnlyOpen();
   std::vector<std::string> orphans;
   if (ok) {
      pkgCache &C = *CF.GetPkgCache();
      pkgDepCache &D = *CF.GetDepCache();
      ManualRootSet roots(D);
      D.MarkAndSweep(roots);
      for (pkgCache::PkgIterator P = C.PkgBegin(); !P.end(); ++P)
         if (!P.CurrentVer().end() && D[P].Garbage)
            orphans.push_back(P.FullName(true));
   }
   _config->Set("APT::Install-Recommends", "false");
   _config->Set("APT::Install-Suggests", "false");
   if (!ok)
      return dupJSON("{\"error\":" + jsonEscape(errorsToString()) + "}");
   return dupJSON(listJSON("orphans", orphans));
}

void aptplan_free(char *ptr) {
   std::free(ptr);
}

} // extern "C"

#else // LIBAPT

extern "C" {

char *aptplan_resolve(const AptRequest *req, AptSession **session) {
   (void) req;
   if (session != nullptr)
      *session = nullptr;
   const char *msg = "{\"error\":\"pacdpkg was built without libapt support\",\"problems\":[],\"actions\":[]}";
   char *p = static_cast<char *>(std::malloc(std::strlen(msg) + 1));
   if (p != nullptr)
      std::memcpy(p, msg, std::strlen(msg) + 1);
   return p;
}

char *aptplan_execute(AptSession *session, int download_only) {
   (void) session;
   (void) download_only;
   const char *msg = "{\"error\":\"pacdpkg was built without libapt support\",\"problems\":[],\"actions\":[]}";
   char *p = static_cast<char *>(std::malloc(std::strlen(msg) + 1));
   if (p != nullptr)
      std::memcpy(p, msg, std::strlen(msg) + 1);
   return p;
}

void aptplan_close(AptSession *session) {
   (void) session;
}

char *aptplan_nativearch(void) {
   const char *msg = "{\"error\":\"pacdpkg was built without libapt support\"}";
   char *p = static_cast<char *>(std::malloc(std::strlen(msg) + 1));
   if (p != nullptr)
      std::memcpy(p, msg, std::strlen(msg) + 1);
   return p;
}

void aptplan_configure(const char *arch, const char *cachedir,
                       const char **architectures, int narchitectures) {
   (void) arch;
   (void) cachedir;
   (void) architectures;
   (void) narchitectures;
}

char *aptplan_unneeded(const AptDepQuery *q) {
   (void) q;
   const char *msg = "{\"error\":\"pacdpkg was built without libapt support\"}";
   char *p = static_cast<char *>(std::malloc(std::strlen(msg) + 1));
   if (p != nullptr)
      std::memcpy(p, msg, std::strlen(msg) + 1);
   return p;
}

char *aptplan_policy(const AptDepQuery *q) {
   (void) q;
   const char *msg = "{\"error\":\"pacdpkg was built without libapt support\"}";
   char *p = static_cast<char *>(std::malloc(std::strlen(msg) + 1));
   if (p != nullptr)
      std::memcpy(p, msg, std::strlen(msg) + 1);
   return p;
}

char *aptplan_checkdeps(const AptDepQuery *q) {
   (void) q;
   const char *msg = "{\"error\":\"pacdpkg was built without libapt support\"}";
   char *p = static_cast<char *>(std::malloc(std::strlen(msg) + 1));
   if (p != nullptr)
      std::memcpy(p, msg, std::strlen(msg) + 1);
   return p;
}

char *aptplan_checkdb(void) {
   const char *msg = "{\"error\":\"pacdpkg was built without libapt support\"}";
   char *p = static_cast<char *>(std::malloc(std::strlen(msg) + 1));
   if (p != nullptr)
      std::memcpy(p, msg, std::strlen(msg) + 1);
   return p;
}

char *aptplan_orphans(int ignore_optdeps) {
   (void) ignore_optdeps;
   const char *msg = "{\"error\":\"pacdpkg was built without libapt support\"}";
   char *p = static_cast<char *>(std::malloc(std::strlen(msg) + 1));
   if (p != nullptr)
      std::memcpy(p, msg, std::strlen(msg) + 1);
   return p;
}

void aptplan_free(char *ptr) {
   std::free(ptr);
}

} // extern "C"

#endif
