// Standalone regression test for legacy callbacks, without game assets or a GPU.
// Build/run with tests/draw_repaint.sh from the legacy directory.
#include <assert.h>
#include <stdio.h>
#include <string.h>
#include "defs.h"
#include "GAME1_2.h"
#include "GAME2_3.h"
#include "GAME3.h"
#include "GAME3_1.h"
#include "client__draw__plasma.h"
#include "client__draw__bubbledraw.h"
#include "client__draw__partscrn.h"

static int paints, spawns, deletes, random_calls;
uint32_t gameFrame(void) { return 1; }
uint32_t gameFPS(void) { return 30; }
void sub_4B6720(int2* p, int color, int size, char blur) { paints++; }
void nox_client_drawSetColor_434460(int color) {}
void nox_xxx_drawPointMB_499B70(int x, int y, int size) { paints++; }
void nox_xxx_spriteDeleteStatic_45A4E0_drawable(nox_drawable* p) { deletes++; }
void nox_xxx_spriteTransparentDecay_49B950(nox_drawable* p, int lifetime) {}
void sub_431700(uint64_t* p) { deletes++; }
int nox_common_randomIntMinMax_415FF0(int min, int max, const char* file, int line) {
	random_calls++;
	return max;
}
nox_screenParticle* nox_client_newScreenParticle_431540(int a, int b, int c, int d, int e, int f,
	char g, char h, char i, char j) {
	spawns++;
	return NULL;
}

// Plasma stores its animation history outside the drawable. Stub the curve
// geometry and rasterizer so the test can check that history independently.
uint32_t dword_5d4594_1316408, dword_5d4594_1316412;
static uint32_t plasma_mem[660], trig_mem[1024];
void* mem_getPtr(uintptr_t base, uintptr_t off) {
	if (base == 0x5D4594) {
		assert(off >= 1313828 && off < 1313828 + sizeof(plasma_mem));
		return (char*)plasma_mem + off - 1313828;
	}
	assert(base == 0x587000 && off >= 194136 && off < 194136 + sizeof(trig_mem));
	return (char*)trig_mem + off - 194136;
}
uint32_t* mem_getU32Ptr(uintptr_t base, uintptr_t off) { return mem_getPtr(base, off); }
uint8_t* mem_getU8Ptr(uintptr_t base, uintptr_t off) { return mem_getPtr(base, off); }
float* mem_getFloatPtr(uintptr_t base, uintptr_t off) { return mem_getPtr(base, off); }
uint32_t nox_color_rgb_4344A0(int r, int g, int b) { return 1; }
int nox_float2int(float f) { return (int)f; }
void sub_4BA670(int a, int b, int c, int d, int e) { dword_5d4594_1316408 = 1; }
int sub_4BE800(int a) { return 0; }
char sub_4BE810(int a, int b, int c, char d) { return 0; }
void sub_4BEAD0(int2* a, int2* b, int2* c, int2* d, int e, int f) { paints++; }

static void test_plasma(void) {
	*getMemU32Ptr(0x5D4594, 1316404) = 1; // initialized
	for (int i = 0; i < 3; i++) {
		for (int j = 0; j < 2; j++) {
			int off = 28 * (30 * i + j);
			*getMemU32Ptr(0x5D4594, 1313908 + off) = 10;
			*getMemU32Ptr(0x5D4594, 1313900 + off) = 3;
		}
	}
	uint32_t saved[660];
	memcpy(saved, plasma_mem, sizeof(saved));
	int drawn = paints, random = random_calls;
	nox_draw_repaint = 1;
	for (int i = 0; i < 5; i++) {
		sub_4BA230(0, 0, 0, 100, 100);
		assert(memcmp(saved, plasma_mem, sizeof(saved)) == 0);
	}
	assert(paints == drawn + 15 && random_calls == random);
	nox_draw_repaint = 0;
	sub_4BA230(0, 0, 0, 100, 100);
	assert(*getMemU32Ptr(0x5D4594, 1313908) == 9);
}

int main(void) {
	nox_draw_viewport_t vp = {0};
	// The callbacks consume the legacy viewport layout directly.
	((int*)&vp)[8] = 640;
	((int*)&vp)[9] = 480;
	nox_screenParticle screen = {0};
	screen.field_24 = 100 << 16;
	screen.field_28 = 100 << 16;
	screen.field_16 = 1 << 16;
	screen.field_36 = 2;
	screen.field_32 = 1;
	screen.field_40[0] = 3;
	screen.field_40[1] = 3;
	screen.field_40[3] = 3;
	nox_drawable bubble = {0};
	((int*)&bubble)[3] = 100;
	((int*)&bubble)[4] = 100;
	((char*)&bubble)[440] = 3;
	((char*)&bubble)[442] = 3;
	((char*)&bubble)[443] = 3;
	((char*)&bubble)[445] = 3;
	((char*)&bubble)[446] = 1;
	nox_screenParticle saved_screen = screen;
	nox_drawable saved_bubble = bubble;
	nox_draw_repaint = 1;
	for (int i = 0; i < 5; i++) {
		nox_client_screenParticleDraw_489700(&vp, &screen);
		nox_thing_bubble_draw((uint32_t*)&vp, &bubble);
		assert(memcmp(&screen, &saved_screen, sizeof(screen)) == 0);
		assert(memcmp(&bubble, &saved_bubble, sizeof(bubble)) == 0);
	}
	assert(paints == 20);
	assert(spawns == 0 && deletes == 0 && random_calls == 0);
	screen.field_24 = -1;
	nox_client_screenParticleDraw_489700(&vp, &screen);
	assert(deletes == 0);
	screen = saved_screen;
	nox_draw_repaint = 0;
	nox_client_screenParticleDraw_489700(&vp, &screen);
	nox_thing_bubble_draw((uint32_t*)&vp, &bubble);
	assert(screen.field_24 == 101 << 16);
	assert(((short*)&bubble)[52] == 1);
	assert(spawns == 1 && random_calls > 0);
	test_plasma();
	puts("legacy repaint regression checks passed");
}
