package render

import (
	"fmt"
	"testing"

	"github.com/wildalley/framely/internal/doc"
)

func TestDimmerMatchesUnionOfClippedRectangles(t *testing.T) {
	for name, areas := range map[string][]doc.Rect{
		"empty":      nil,
		"overlap":    {{X: 2, Y: 3, W: 12, H: 9}, {X: 9, Y: 7, W: 13, H: 16}},
		"touching":   {{X: 2, Y: 2, W: 8, H: 8}, {X: 10, Y: 2, W: 8, H: 8}},
		"nested":     {{X: 1, Y: 1, W: 28, H: 28}, {X: 10, Y: 10, W: 4, H: 4}},
		"clipped":    {{X: -5, Y: -3, W: 16, H: 13}, {X: 27, Y: 25, W: 20, H: 10}, {X: 50, Y: 50, W: 9, H: 9}},
		"reversed":   {{X: 15, Y: 19, W: -10, H: -12}},
		"degenerate": {{X: 4, Y: 5, W: 0, H: 10}, {X: 7, Y: 5, W: 9, H: 0}},
	} {
		t.Run(name, func(t *testing.T) {
			path := dimmerPath(areas, 32, 32)
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					px, py := float64(x)+.5, float64(y)+.5
					covered := false
					for _, r := range areas {
						if r.W < 0 {
							r.X, r.W = r.X+r.W, -r.W
						}
						if r.H < 0 {
							r.Y, r.H = r.Y+r.H, -r.H
						}
						covered = covered || px >= r.X && px < r.X+r.W && py >= r.Y && py < r.Y+r.H
					}
					if path.Contains(float32(px), float32(py)) == covered {
						t.Fatalf("wrong mask at %.1f,%.1f: highlighted=%v", px, py, covered)
					}
				}
			}
		})
	}
}

func TestDimmerCacheTracksGeometryChanges(t *testing.T) {
	var cache DimmerCache
	areas := []doc.Rect{{X: 2, Y: 3, W: 12, H: 9}}
	first := cache.pathFor(areas, 32, 32)
	if cache.pathFor(areas, 32, 32) != first {
		t.Fatal("unchanged geometry rebuilt")
	}
	areas[0].X = 18
	second := cache.pathFor(areas, 32, 32)
	if second == first || second.Contains(20, 5) || !second.Contains(5, 5) {
		t.Fatal("in-place drag kept a stale mask")
	}
	if cache.pathFor(areas, 40, 32) == second {
		t.Fatal("size change kept a stale mask")
	}
	if !cache.pathFor(nil, 32, 32).Contains(20, 5) {
		t.Fatal("removing highlights kept a stale hole")
	}
}

func benchmarkAreas(n int) []doc.Rect {
	areas := make([]doc.Rect, n)
	for i := range areas {
		areas[i] = doc.Rect{X: float64(i*10 + 1), Y: float64(i*10 + 1), W: 3, H: 3}
	}
	return areas
}

func BenchmarkDimmerPath(b *testing.B) {
	for _, n := range []int{20, 100, 300} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			areas := benchmarkAreas(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = dimmerPath(areas, 4096, 4096)
			}
		})
	}
}

func BenchmarkDimmerCache(b *testing.B) {
	areas := benchmarkAreas(300)
	var cache DimmerCache
	cache.pathFor(areas, 4096, 4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.pathFor(areas, 4096, 4096)
	}
}
