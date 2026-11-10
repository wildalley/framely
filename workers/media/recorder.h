#ifndef SCREENPIN_RECORDER_H
#define SCREENPIN_RECORDER_H
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
struct sp_recorder;
struct sp_record_options { const char *path; int fps, max_edge; const char *codec; };
struct sp_recorder *sp_record_new(struct sp_record_options options);
void sp_record_free(struct sp_recorder *r);
int sp_record_select(struct sp_recorder *r, int x, int y, int width, int height, int source_width, int source_height);
int sp_record_begin(struct sp_recorder *r, int width, int height, int64_t now);
int sp_record_frame(struct sp_recorder *r, const uint8_t *pixels, int width, int height, int stride, int format);
int sp_record_capture(struct sp_recorder *r, const uint8_t *pixels, int width, int height, int stride, int format);
int sp_record_tick(struct sp_recorder *r, int64_t now);
int sp_record_pause(struct sp_recorder *r, int paused, int64_t now);
int sp_record_finish(struct sp_recorder *r, int64_t now);
int sp_record_active(const struct sp_recorder *r);
const char *sp_record_error(const struct sp_recorder *r);
void sp_record_status(struct sp_recorder *r, int64_t now, char *json, unsigned size);
#ifdef __cplusplus
}
#endif
#endif
