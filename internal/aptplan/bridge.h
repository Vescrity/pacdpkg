#ifndef PACDPKG_APTPLAN_BRIDGE_H
#define PACDPKG_APTPLAN_BRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

enum {
   APT_OP_INSTALL = 0,
   APT_OP_REMOVE = 1,
   APT_OP_UPGRADE = 2
};

enum {
   APT_FLAG_NEEDED = 1 << 0,
   APT_FLAG_PURGE = 1 << 1,
   APT_FLAG_RECURSIVE = 1 << 2,
   APT_FLAG_CASCADE = 1 << 3,
   APT_FLAG_FORCE_DEPS = 1 << 4,
   APT_FLAG_DOWNGRADE = 1 << 5,
   APT_FLAG_LOCK = 1 << 6,
   APT_FLAG_UPGRADE_ONLY = 1 << 7,
   APT_FLAG_ASDEPS = 1 << 8,
   APT_FLAG_ASEXPLICIT = 1 << 9,
   APT_FLAG_OVERRIDE_HELD = 1 << 10
};

typedef struct {
   int op;
   unsigned int flags;
   const char **names;
   const char **versions;
   const char **repos;
   const char **debs;
   int ntargets;
   const char **ignorepkgs;
   int nignorepkgs;
   const char **ignoregroups;
   int nignoregroups;
   const char *arch;
   const char *cachedir;
   const char **architectures;
   int narchitectures;
} AptRequest;

typedef struct {
   const char **names;
   const char **ops;
   const char **versions;
   int ntargets;
} AptDepQuery;

typedef struct AptSession AptSession;

char *aptplan_resolve(const AptRequest *req, AptSession **session);
char *aptplan_execute(AptSession *session, int download_only);
void aptplan_close(AptSession *session);
void aptplan_free(char *ptr);

char *aptplan_checkdeps(const AptDepQuery *q);
char *aptplan_checkdb(void);
char *aptplan_orphans(int ignore_optdeps);

#ifdef __cplusplus
}
#endif

#endif
