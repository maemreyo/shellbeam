//go:build darwin

package dyld

const interposeSource = `
#include <dirent.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>

#define SB_PROTOCOL 1
#define SB_MAX_PATH 4096
#define SB_READ 1
#define SB_META 2
#define SB_DIR 3
#define SB_WRITE 4
#define SB_EXEC 5
#define SB_LIBRARY 6
#define SB_ACTIVE 7

struct __attribute__((packed)) sb_header {
  uint8_t version;
  uint8_t event_class;
  uint16_t flags;
  uint32_t pid;
  uint32_t path_len;
};

static int sb_fd = -1;
static int sb_guard = 0;

static int sb_enter_guard(void) {
  return __sync_lock_test_and_set(&sb_guard, 1) == 0;
}

static void sb_leave_guard(void) {
  __sync_lock_release(&sb_guard);
}
static void sb_emit(uint8_t event_class, const char *path) {
  if (sb_fd < 0) return;
  if (!sb_enter_guard()) return;
  size_t n = 0;
  if (event_class != SB_ACTIVE) {
    if (!path) { sb_leave_guard(); return; }
    n = strnlen(path, SB_MAX_PATH + 1);
    if (n == 0 || n > SB_MAX_PATH) { sb_leave_guard(); return; }
  }
  unsigned char buffer[sizeof(struct sb_header) + SB_MAX_PATH];
  struct sb_header h;
  h.version = SB_PROTOCOL;
  h.event_class = event_class;
  h.flags = 0;
  h.pid = (uint32_t)getpid();
  h.path_len = (uint32_t)n;
  memcpy(buffer, &h, sizeof(h));
  if (n > 0) memcpy(buffer + sizeof(h), path, n);
  (void)send(sb_fd, buffer, sizeof(h) + n, MSG_DONTWAIT);
  sb_leave_guard();
}

__attribute__((constructor)) static void sb_init(void) {
  if (!sb_enter_guard()) return;
  const char *protocol = getenv("SHELLBEAM_TRACE_PROTOCOL");
  const char *socket_path = getenv("SHELLBEAM_TRACE_SOCKET");
  if (!protocol || strcmp(protocol, "1") != 0 || !socket_path) { sb_leave_guard(); return; }
  size_t n = strnlen(socket_path, sizeof(((struct sockaddr_un *)0)->sun_path));
  if (n == 0 || n >= sizeof(((struct sockaddr_un *)0)->sun_path)) { sb_leave_guard(); return; }
  int fd = socket(AF_UNIX, SOCK_DGRAM, 0);
  if (fd < 0) { sb_leave_guard(); return; }
  struct sockaddr_un addr;
  memset(&addr, 0, sizeof(addr));
  addr.sun_family = AF_UNIX;
  memcpy(addr.sun_path, socket_path, n + 1);
  if (connect(fd, (struct sockaddr *)&addr, sizeof(addr)) != 0) { close(fd); sb_leave_guard(); return; }
  int flags = fcntl(fd, F_GETFL, 0);
  if (flags >= 0) (void)fcntl(fd, F_SETFL, flags | O_NONBLOCK);
  sb_fd = fd;
  sb_leave_guard();
  sb_emit(SB_ACTIVE, NULL);
}

__attribute__((destructor)) static void sb_fini(void) {
  if (sb_fd >= 0) close(sb_fd);
  sb_fd = -1;
}

static int sb_open(const char *path, int flags, ...) {
  uint8_t cls = (flags & (O_WRONLY|O_RDWR|O_CREAT|O_TRUNC|O_APPEND)) ? SB_WRITE : SB_READ;
  sb_emit(cls, path);
  if (flags & O_CREAT) {
    va_list ap; va_start(ap, flags); mode_t mode = (mode_t)va_arg(ap, int); va_end(ap);
    return open(path, flags, mode);
  }
  return open(path, flags);
}

static int sb_stat(const char *path, struct stat *st) {
  sb_emit(SB_META, path);
  return stat(path, st);
}

static DIR *sb_opendir(const char *path) {
  sb_emit(SB_DIR, path);
  return opendir(path);
}

static int sb_execve(const char *path, char *const argv[], char *const envp[]) {
  sb_emit(SB_EXEC, path);
  return execve(path, argv, envp);
}

static void *sb_dlopen(const char *path, int mode) {
  if (path) sb_emit(SB_LIBRARY, path);
  return dlopen(path, mode);
}

#define DYLD_INTERPOSE(_replacement,_replacee) \
__attribute__((used)) static struct { const void *replacement; const void *replacee; } _interpose_##_replacee \
__attribute__((section("__DATA,__interpose"))) = { (const void *)(uintptr_t)&_replacement, (const void *)(uintptr_t)&_replacee };

DYLD_INTERPOSE(sb_open, open)
DYLD_INTERPOSE(sb_stat, stat)
DYLD_INTERPOSE(sb_opendir, opendir)
DYLD_INTERPOSE(sb_execve, execve)
DYLD_INTERPOSE(sb_dlopen, dlopen)
`
