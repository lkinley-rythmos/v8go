#ifndef V8GO_SNAPSHOT_H
#define V8GO_SNAPSHOT_H
#include "v8go.h"
#ifdef __cplusplus
extern "C" {
#endif
typedef struct {
  char *data;
  int length;
  RtnError error;
} RtnSnapshot;
typedef struct {
  ContextPtr ptr;
  RtnError error;
} RtnSnapshotContext;
RtnSnapshot CreateLibrarySnapshot(const char *source, char **names, int count);
void FreeLibrarySnapshot(char *data);
uint32_t SnapshotCompatibilityTag();
IsolatePtr NewSnapshotIsolate(const char *data, int length);
RtnSnapshotContext NewLibraryContext(IsolatePtr iso, TemplatePtr global,
                                     int ref);
RtnValue LibraryContextData(ContextPtr ctx, int index);
#ifdef __cplusplus
}
#endif
#endif
