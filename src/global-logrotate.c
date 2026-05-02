#define _XOPEN_SOURCE 700
#include <errno.h>
#include <fnmatch.h>
#include <getopt.h>
#include <libgen.h>
#include <limits.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <time.h>
#include <unistd.h>
#include <dirent.h>

#ifndef PATH_MAX
#define PATH_MAX 4096
#endif

#define VERSION "2.1.15-c1"
#define DEFAULT_DIR "/var/log/apps"
#define DEFAULT_LOG_FILE "/var/log/global-sys-utils/global-logrotate.log"
#define MAIN_CONFIG_FILE "/etc/global-sys-utils/global.conf"
#define CONFIG_DROPIN_DIR "/etc/global-sys-utils/global.conf.d"

struct config {
    char log_dir[PATH_MAX];
    char pattern[256];
    char old_logs_dir[PATH_MAX];
    char exclude_file[PATH_MAX];
    char date_suffix[64];
    char log_file[PATH_MAX];
    int dry_run;
    int parallel_jobs;
    int log_level;
};

struct strlist { char **items; size_t len; size_t cap; };

static FILE *log_fp = NULL;
static int log_level = 1;

static void die(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vfprintf(stderr, fmt, ap);
    va_end(ap);
    fputc('\n', stderr);
    exit(1);
}

static void log_msg(int level, const char *fmt, ...) {
    if (!log_fp || level > log_level) return;
    time_t now = time(NULL);
    struct tm tmv;
    localtime_r(&now, &tmv);
    char ts[32];
    strftime(ts, sizeof(ts), "%Y-%m-%d %H:%M:%S", &tmv);
    const char *ls = level == 0 ? "ERROR" : level == 2 ? "DEBUG" : "INFO";
    fprintf(log_fp, "[%s] [%s] ", ts, ls);
    va_list ap;
    va_start(ap, fmt);
    vfprintf(log_fp, fmt, ap);
    va_end(ap);
    fputc('\n', log_fp);
    fflush(log_fp);
}

static void mkdir_p(const char *path, mode_t mode) {
    char tmp[PATH_MAX];
    snprintf(tmp, sizeof(tmp), "%s", path);
    size_t len = strlen(tmp);
    if (len == 0) return;
    if (tmp[len - 1] == '/') tmp[len - 1] = 0;
    for (char *p = tmp + 1; *p; p++) {
        if (*p == '/') {
            *p = 0;
            mkdir(tmp, mode);
            *p = '/';
        }
    }
    mkdir(tmp, mode);
}

static void ensure_parent_dir(const char *path) {
    char tmp[PATH_MAX];
    snprintf(tmp, sizeof(tmp), "%s", path);
    char *d = dirname(tmp);
    mkdir_p(d, 0755);
}

static void list_add(struct strlist *l, const char *s) {
    if (l->len == l->cap) {
        l->cap = l->cap ? l->cap * 2 : 16;
        l->items = realloc(l->items, l->cap * sizeof(char *));
        if (!l->items) die("out of memory");
    }
    l->items[l->len++] = strdup(s);
}

static void list_free(struct strlist *l) {
    for (size_t i = 0; i < l->len; i++) free(l->items[i]);
    free(l->items);
}

static char *trim(char *s) {
    while (*s == ' ' || *s == '\t' || *s == '\n' || *s == '\r') s++;
    char *e = s + strlen(s);
    while (e > s && (e[-1] == ' ' || e[-1] == '\t' || e[-1] == '\n' || e[-1] == '\r')) *--e = 0;
    if ((*s == '"' && e > s && e[-1] == '"') || (*s == '\'' && e > s && e[-1] == '\'')) { s++; e[-1] = 0; }
    return s;
}

static void set_config_value(struct config *cfg, const char *k, const char *v) {
    if (!strcmp(k, "LOG_DIR")) snprintf(cfg->log_dir, sizeof(cfg->log_dir), "%s", v);
    else if (!strcmp(k, "PATTERN")) snprintf(cfg->pattern, sizeof(cfg->pattern), "%s", v);
    else if (!strcmp(k, "OLD_LOGS_DIR")) snprintf(cfg->old_logs_dir, sizeof(cfg->old_logs_dir), "%s", v);
    else if (!strcmp(k, "EXCLUDE_FILE")) snprintf(cfg->exclude_file, sizeof(cfg->exclude_file), "%s", v);
    else if (!strcmp(k, "LOG_FILE")) snprintf(cfg->log_file, sizeof(cfg->log_file), "%s", v);
    else if (!strcmp(k, "PARALLEL_JOBS")) cfg->parallel_jobs = atoi(v);
    else if (!strcmp(k, "DRY_RUN")) cfg->dry_run = (!strcasecmp(v, "true") || !strcmp(v, "1") || !strcasecmp(v, "yes"));
    else if (!strcmp(k, "LOG_LEVEL")) cfg->log_level = (!strcasecmp(v, "debug") || !strcmp(v, "2")) ? 2 : (!strcasecmp(v, "error") || !strcmp(v, "0")) ? 0 : 1;
}

static void load_config_file(struct config *cfg, const char *path) {
    FILE *f = fopen(path, "r");
    if (!f) return;
    char line[2048];
    while (fgets(line, sizeof(line), f)) {
        char *s = trim(line);
        if (!*s || *s == '#' || *s == ';') continue;
        char *eq = strchr(s, '=');
        if (!eq) continue;
        *eq = 0;
        char *k = trim(s), *v = trim(eq + 1);
        set_config_value(cfg, k, v);
    }
    fclose(f);
}

static void load_config(struct config *cfg) {
    load_config_file(cfg, MAIN_CONFIG_FILE);
    DIR *d = opendir(CONFIG_DROPIN_DIR);
    if (!d) return;
    struct dirent *de;
    while ((de = readdir(d))) {
        if (!strstr(de->d_name, ".conf")) continue;
        char p[PATH_MAX];
        snprintf(p, sizeof(p), "%s/%s", CONFIG_DROPIN_DIR, de->d_name);
        load_config_file(cfg, p);
    }
    closedir(d);
}

static void show_usage(void) {
    puts("Usage: global-logrotate [OPTIONS]");
    puts("");
    puts("A fast log rotation utility written in C.");
    puts("");
    puts("Options:");
    puts("  -H                  Use full timestamp format (YYYYMMDDTHH:MM:SS)");
    puts("  -D                  Use date-only format (YYYYMMDD)");
    puts("  --pattern <glob>    File pattern to rotate (default: *.log)");
    puts("  -p <path>           Specify custom log directory (default: /var/log/apps)");
    puts("  -n                  Dry-run mode (no changes made)");
    puts("  --exclude-from      Path to file containing exclude patterns");
    puts("  -o <path>           Specify old_logs directory (default: <logdir>/old_logs)");
    puts("  --parallel N        Accepted for compatibility; rotation is sequential in C c1");
    puts("  --log-file <path>   Path to log file");
    puts("  --log-level <level> Log level: error, info, debug");
    puts("  --version           Show version");
    puts("  -h                  Show this help");
    puts("");
    puts("Note: --encrypt, --read, --pass-gen, and --pass-reset are not included in the C c1 build.");
}

static void now_suffix(char *buf, size_t n, int full) {
    time_t now = time(NULL);
    struct tm tmv;
    localtime_r(&now, &tmv);
    strftime(buf, n, full ? "%Y%m%dT%H:%M:%S" : "%Y%m%d", &tmv);
}

static void load_excludes(const char *path, struct strlist *ex) {
    if (!path || !*path) return;
    FILE *f = fopen(path, "r");
    if (!f) die("Error: Exclude file '%s' does not exist.", path);
    char line[1024];
    printf("Excluding patterns from: %s\n", path);
    while (fgets(line, sizeof(line), f)) {
        char *s = trim(line);
        if (*s && *s != '#') {
            printf("  - %s\n", s);
            list_add(ex, s);
        }
    }
    fclose(f);
}

static int excluded(const char *name, const char *path, const struct strlist *ex) {
    for (size_t i = 0; i < ex->len; i++) {
        if (fnmatch(ex->items[i], name, 0) == 0 || fnmatch(ex->items[i], path, 0) == 0) return 1;
    }
    return 0;
}

static void find_files(const char *dir, const char *pattern, const struct strlist *ex, struct strlist *files) {
    DIR *d = opendir(dir);
    if (!d) return;
    struct dirent *de;
    while ((de = readdir(d))) {
        if (!strcmp(de->d_name, ".") || !strcmp(de->d_name, "..")) continue;
        char p[PATH_MAX];
        snprintf(p, sizeof(p), "%s/%s", dir, de->d_name);
        struct stat st;
        if (lstat(p, &st) != 0) continue;
        if (S_ISDIR(st.st_mode)) {
            find_files(p, pattern, ex, files);
        } else if (S_ISREG(st.st_mode) && fnmatch(pattern, de->d_name, 0) == 0 && !excluded(de->d_name, p, ex)) {
            list_add(files, p);
        }
    }
    closedir(d);
}

static int copy_file(const char *src, const char *dst, mode_t mode) {
    FILE *in = fopen(src, "rb");
    if (!in) return -1;
    ensure_parent_dir(dst);
    FILE *out = fopen(dst, "wb");
    if (!out) { fclose(in); return -1; }
    char buf[65536];
    size_t n;
    int rc = 0;
    while ((n = fread(buf, 1, sizeof(buf), in)) > 0) {
        if (fwrite(buf, 1, n, out) != n) { rc = -1; break; }
    }
    if (ferror(in)) rc = -1;
    fclose(in);
    if (fclose(out) != 0) rc = -1;
    chmod(dst, mode & 0777);
    return rc;
}

static void rotate_one(const char *path, const struct config *cfg) {
    char olddir[PATH_MAX];
    if (cfg->old_logs_dir[0]) snprintf(olddir, sizeof(olddir), "%s", cfg->old_logs_dir);
    else snprintf(olddir, sizeof(olddir), "%s/old_logs", cfg->log_dir);
    mkdir_p(olddir, 0755);

    const char *base = strrchr(path, '/');
    base = base ? base + 1 : path;
    char dst[PATH_MAX];
    snprintf(dst, sizeof(dst), "%s/%s.%s", olddir, base, cfg->date_suffix);

    printf("Rotating: %s -> %s\n", path, dst);
    log_msg(1, "Rotating: %s -> %s", path, dst);
    if (cfg->dry_run) return;

    struct stat st;
    if (stat(path, &st) != 0) { log_msg(0, "stat failed for %s: %s", path, strerror(errno)); return; }
    if (copy_file(path, dst, st.st_mode) != 0) { log_msg(0, "copy failed %s -> %s: %s", path, dst, strerror(errno)); return; }
    FILE *f = fopen(path, "w");
    if (f) fclose(f);
    chmod(path, st.st_mode & 0777);
    chown(path, st.st_uid, st.st_gid);
}

int main(int argc, char **argv) {
    struct config cfg;
    memset(&cfg, 0, sizeof(cfg));
    snprintf(cfg.log_dir, sizeof(cfg.log_dir), "%s", DEFAULT_DIR);
    snprintf(cfg.pattern, sizeof(cfg.pattern), "*.log");
    snprintf(cfg.log_file, sizeof(cfg.log_file), "%s", DEFAULT_LOG_FILE);
    cfg.parallel_jobs = 4;
    cfg.log_level = 1;
    load_config(&cfg);

    int full_time = 0, date_only = 0;
    static struct option opts[] = {
        {"pattern", required_argument, 0, 1000}, {"exclude-from", required_argument, 0, 1001},
        {"parallel", required_argument, 0, 1002}, {"log-file", required_argument, 0, 1003},
        {"log-level", required_argument, 0, 1004}, {"version", no_argument, 0, 1005},
        {"encrypt", no_argument, 0, 1006}, {"read", required_argument, 0, 1007},
        {"pass-gen", no_argument, 0, 1008}, {"pass-reset", no_argument, 0, 1009},
        {"help", no_argument, 0, 'h'}, {0,0,0,0}
    };
    int c;
    while ((c = getopt_long(argc, argv, "HDp:no:h", opts, NULL)) != -1) {
        switch (c) {
            case 'H': full_time = 1; break;
            case 'D': date_only = 1; break;
            case 'p': snprintf(cfg.log_dir, sizeof(cfg.log_dir), "%s", optarg); break;
            case 'n': cfg.dry_run = 1; break;
            case 'o': snprintf(cfg.old_logs_dir, sizeof(cfg.old_logs_dir), "%s", optarg); break;
            case 'h': show_usage(); return 0;
            case 1000: snprintf(cfg.pattern, sizeof(cfg.pattern), "%s", optarg); break;
            case 1001: snprintf(cfg.exclude_file, sizeof(cfg.exclude_file), "%s", optarg); break;
            case 1002: cfg.parallel_jobs = atoi(optarg); break;
            case 1003: snprintf(cfg.log_file, sizeof(cfg.log_file), "%s", optarg); break;
            case 1004: set_config_value(&cfg, "LOG_LEVEL", optarg); break;
            case 1005: printf("global-logrotate version %s\n", VERSION); return 0;
            case 1006: case 1007: case 1008: case 1009:
                die("This C c1 build does not yet support encryption/password/read mode. Use the Go main branch for those features.");
            default: show_usage(); return 1;
        }
    }
    if (argc == 1) { show_usage(); return 0; }
    now_suffix(cfg.date_suffix, sizeof(cfg.date_suffix), full_time && !date_only);

    ensure_parent_dir(cfg.log_file);
    log_fp = fopen(cfg.log_file, "a");
    log_level = cfg.log_level;
    log_msg(1, "global-logrotate %s started", VERSION);

    struct strlist excludes = {0}, files = {0};
    load_excludes(cfg.exclude_file, &excludes);
    find_files(cfg.log_dir, cfg.pattern, &excludes, &files);
    if (files.len == 0) {
        printf("No files matching pattern '%s' found in %s\n", cfg.pattern, cfg.log_dir);
        log_msg(1, "No files matching pattern '%s' found in %s", cfg.pattern, cfg.log_dir);
        list_free(&excludes); list_free(&files);
        if (log_fp) fclose(log_fp);
        return 0;
    }
    printf("Found %zu files to rotate\n", files.len);
    for (size_t i = 0; i < files.len; i++) rotate_one(files.items[i], &cfg);
    log_msg(1, "Rotation completed");
    list_free(&excludes); list_free(&files);
    if (log_fp) fclose(log_fp);
    return 0;
}
