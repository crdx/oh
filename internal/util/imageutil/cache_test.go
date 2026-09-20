package imageutil

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/png"
	"testing"

	"crdx.org/io/pkg/tool"
)

func TestBoundedImageCacheEvictsTheLeastRecentlyUsedImage(t *testing.T) {
	cache := newBoundedImageCache(2, 10)
	firstKey := imageCacheKey{digest: sha256.Sum256([]byte("first")), mediaType: mediaTypePNG}
	secondKey := imageCacheKey{digest: sha256.Sum256([]byte("second")), mediaType: mediaTypePNG}
	thirdKey := imageCacheKey{digest: sha256.Sum256([]byte("third")), mediaType: mediaTypePNG}

	cache.store(firstKey, tool.Image{Data: []byte{1}})
	cache.store(secondKey, tool.Image{Data: []byte{2}})
	if _, isFound := cache.get(firstKey); !isFound {
		t.Fatal("the first image was not cached")
	}
	cache.store(thirdKey, tool.Image{Data: []byte{3}})

	if _, isFound := cache.get(secondKey); isFound {
		t.Error("the least recently used image was not evicted")
	}
	if _, isFound := cache.get(firstKey); !isFound {
		t.Error("the recently used image was evicted")
	}
	if _, isFound := cache.get(thirdKey); !isFound {
		t.Error("the new image was not cached")
	}
	if cache.storedBytes != 2 {
		t.Errorf("cache holds %d bytes, want 2", cache.storedBytes)
	}
}

func TestBoundedImageCacheLimitsItsNumberOfImages(t *testing.T) {
	cache := newBoundedImageCache(10, 2)
	firstKey := imageCacheKey{digest: sha256.Sum256([]byte("entry-first")), mediaType: mediaTypePNG}
	secondKey := imageCacheKey{digest: sha256.Sum256([]byte("entry-second")), mediaType: mediaTypePNG}
	thirdKey := imageCacheKey{digest: sha256.Sum256([]byte("entry-third")), mediaType: mediaTypePNG}

	cache.store(firstKey, tool.Image{Data: []byte{1}})
	cache.store(secondKey, tool.Image{Data: []byte{2}})
	cache.store(thirdKey, tool.Image{Data: []byte{3}})

	if _, isFound := cache.get(firstKey); isFound {
		t.Error("the oldest image was not evicted")
	}
	if len(cache.images) != 2 {
		t.Errorf("cache holds %d images, want 2", len(cache.images))
	}
}

func TestBoundedImageCacheDoesNotStoreAnImageLargerThanItself(t *testing.T) {
	cache := newBoundedImageCache(2, 10)
	key := imageCacheKey{digest: sha256.Sum256([]byte("large")), mediaType: mediaTypePNG}
	data := make([]byte, 1, 3)
	cache.store(key, tool.Image{Data: data})

	if _, isFound := cache.get(key); isFound {
		t.Error("an image larger than the cache was stored")
	}
}

func TestBoundDoesNotCacheAnImageThatCannotBeDecoded(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, MaxEdge+1, 1))); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() < 33 {
		t.Fatal("encoded PNG is unexpectedly short")
	}
	malformed := append(bytes.Clone(encoded.Bytes()[:33]), make([]byte, 1024)...)
	if _, _, err := image.Decode(bytes.NewReader(malformed)); err == nil {
		t.Fatal("the malformed PNG unexpectedly decoded")
	}

	boundedCache.mutex.Lock()
	before := len(boundedCache.images)
	boundedCache.mutex.Unlock()

	subject := tool.Image{MediaType: mediaTypePNG, Data: malformed}
	result := Bound(subject)
	if !bytes.Equal(result.Data, subject.Data) {
		t.Error("the malformed image was changed")
	}

	boundedCache.mutex.Lock()
	after := len(boundedCache.images)
	boundedCache.mutex.Unlock()
	if after != before {
		t.Errorf("cache grew from %d to %d entries", before, after)
	}
}
