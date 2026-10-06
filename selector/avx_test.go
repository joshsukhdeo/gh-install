package selector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectHostAVXLevel(t *testing.T) {
	level := DetectHostAVXLevel()
	assert.Contains(t, []string{"none", "avx", "avx2", "avx512"}, level)
}

func TestAVXLevelRank(t *testing.T) {
	assert.Equal(t, 0, AVXLevelRank("none"))
	assert.Equal(t, 0, AVXLevelRank("unknown"))
	assert.Equal(t, 1, AVXLevelRank("avx"))
	assert.Equal(t, 2, AVXLevelRank("avx2"))
	assert.Equal(t, 3, AVXLevelRank("avx512"))
}

func TestAssetAVXRequirement(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"thorium_128.0.6613.178_AVX512.deb", "avx512"},
		{"thorium_128.0.6613.178_avx-512.tar.gz", "avx512"},
		{"thorium_128.0.6613.178_avx_512.tar.gz", "avx512"},
		{"thorium_128.0.6613.178_AVX2.deb", "avx2"},
		{"llama-b3000-bin-win-avx2-x64.zip", "avx2"},
		{"thorium_128.0.6613.178_AVX.deb", "avx"},
		{"llama-b3000-bin-win-avx-x64.zip", "avx"},
		{"thorium_128.0.6613.178_SSE3.deb", "none"},
		{"llama-b3000-bin-win-noavx-x64.zip", "none"},
		{"llama-b3000-bin-win-no-avx-x64.zip", "none"},
		{"llama-b3000-bin-win-non-avx-x64.zip", "none"},
		{"ripgrep-13.0.0-x86_64-unknown-linux-musl.tar.gz", "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, AssetAVXRequirement(tt.name))
		})
	}
}

func TestIsAVXCompatible(t *testing.T) {
	assert.True(t, IsAVXCompatible("app_AVX512.tar.gz", "avx512"))
	assert.False(t, IsAVXCompatible("app_AVX512.tar.gz", "avx2"))
	assert.False(t, IsAVXCompatible("app_AVX512.tar.gz", "avx"))
	assert.False(t, IsAVXCompatible("app_AVX512.tar.gz", "none"))

	assert.True(t, IsAVXCompatible("app_AVX2.tar.gz", "avx512"))
	assert.True(t, IsAVXCompatible("app_AVX2.tar.gz", "avx2"))
	assert.False(t, IsAVXCompatible("app_AVX2.tar.gz", "avx"))
	assert.False(t, IsAVXCompatible("app_AVX2.tar.gz", "none"))

	assert.True(t, IsAVXCompatible("app_AVX.tar.gz", "avx512"))
	assert.True(t, IsAVXCompatible("app_AVX.tar.gz", "avx2"))
	assert.True(t, IsAVXCompatible("app_AVX.tar.gz", "avx"))
	assert.False(t, IsAVXCompatible("app_AVX.tar.gz", "none"))

	assert.True(t, IsAVXCompatible("app_generic.tar.gz", "none"))
	assert.True(t, IsAVXCompatible("app_generic.tar.gz", "avx"))
	assert.True(t, IsAVXCompatible("app_generic.tar.gz", "avx2"))
	assert.True(t, IsAVXCompatible("app_generic.tar.gz", "avx512"))
}

func TestSelector_AVXPriorityMatching(t *testing.T) {
	items := []*SelectorItem{
		{Name: "thorium_128_AVX512.deb"},
		{Name: "thorium_128_AVX2.deb"},
		{Name: "thorium_128_AVX.deb"},
		{Name: "thorium_128_SSE3.deb"},
	}

	t.Run("HostAVX2_SelectsAVX2", func(t *testing.T) {
		s := &Selector{
			Kind:           Asset,
			Items:          items,
			RegexpMatchers: []string{`.*\.deb`},
			Single:         true,
			AvxLevel:       "avx2",
			Repository:     "alex313031/thorium",
		}
		res, err := s.Run()
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, "thorium_128_AVX2.deb", res[0].Name)
	})

	t.Run("HostAVX_SelectsAVX", func(t *testing.T) {
		s := &Selector{
			Kind:           Asset,
			Items:          items,
			RegexpMatchers: []string{`.*\.deb`},
			Single:         true,
			AvxLevel:       "avx",
			Repository:     "alex313031/thorium",
		}
		res, err := s.Run()
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, "thorium_128_AVX.deb", res[0].Name)
	})

	t.Run("HostNone_SelectsSSE3", func(t *testing.T) {
		s := &Selector{
			Kind:           Asset,
			Items:          items,
			RegexpMatchers: []string{`.*\.deb`},
			Single:         true,
			AvxLevel:       "none",
			Repository:     "alex313031/thorium",
		}
		res, err := s.Run()
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, "thorium_128_SSE3.deb", res[0].Name)
	})

	t.Run("HostAVX512_SelectsAVX512", func(t *testing.T) {
		s := &Selector{
			Kind:           Asset,
			Items:          items,
			RegexpMatchers: []string{`.*\.deb`},
			Single:         true,
			AvxLevel:       "avx512",
			Repository:     "alex313031/thorium",
		}
		res, err := s.Run()
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, "thorium_128_AVX512.deb", res[0].Name)
	})
}
