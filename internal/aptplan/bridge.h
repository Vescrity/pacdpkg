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
   APT_FLAG_UPGRADE_ONLY = 1 << 7
};

typedef struct {
   int op;
   unsigned int flags;
   const char **names;
   const char **versions;
   const char **repos;
   const char **debs;
   int ntargets;
} AptRequest;

typedef struct AptSession AptSession;

char *aptplan_resolve(const AptRequest *req, AptSession **session);
char *aptplan_execute(AptSession *session, int download_only);
void aptplan_close(AptSession *session);
void aptplan_free(char *ptr);

#ifdef __cplusplus
}
#endif

#endif
