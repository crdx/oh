package graphics

import (
	"crypto/sha256"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	cacheBudget   = 256 << 20
	bytesPerPixel = 4

	transmissionStart = openCommand + "a=T,"
	identifierField   = "i="
)

type Cache struct {
	mutex      sync.Mutex
	imageIDs   map[cacheKey]int
	pictures   map[int]*terminalPicture
	heldBytes  int
	placements int
}

type cacheKey struct {
	medium     string
	path       string
	sizeBytes  int64
	modifiedAt int64
	digest     [sha256.Size]byte
	box        Box
}

type terminalPicture struct {
	bytes   int
	lastUse int
	isHeld  bool
}

func NewCache() *Cache {
	return &Cache{
		imageIDs: map[cacheKey]int{},
		pictures: map[int]*terminalPicture{},
	}
}

func (self *Cache) PlacePNG(data []byte, size Size, box Box) ([]string, bool) {
	if len(data) == 0 {
		return nil, false
	}

	key := cacheKey{medium: mediumDirect, digest: sha256.Sum256(data), box: box}

	return self.place(key, size, formatPNG, mediumDirect, encodeBase64(data), box)
}

func (self *Cache) PlaceFile(path string, size Size, box Box) ([]string, bool) {
	if path == "" {
		return nil, false
	}

	payload := encodeBase64([]byte(path))

	info, err := os.Stat(path)
	if err != nil {
		return place(nextImageID(), false, formatPNG, mediumFile, payload, box)
	}

	key := cacheKey{
		medium:     mediumFile,
		path:       path,
		sizeBytes:  info.Size(),
		modifiedAt: info.ModTime().UnixNano(),
		box:        box,
	}

	return self.place(key, size, formatPNG, mediumFile, payload, box)
}

func (self *Cache) Observe(text string) string {
	if self == nil || !strings.Contains(text, transmissionStart) {
		return ""
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	transmissions := map[int]bool{}
	for rest := text; ; {
		start := strings.Index(rest, transmissionStart)
		if start < 0 {
			break
		}
		rest = rest[start+len(transmissionStart):]

		imageID := transmittedID(rest)
		picture, isKnown := self.pictures[imageID]
		if !isKnown {
			continue
		}

		if !picture.isHeld {
			picture.isHeld = true
			self.heldBytes += picture.bytes
		}
		transmissions[imageID] = true
	}

	return self.release(transmissions)
}

func (self *Cache) place(key cacheKey, size Size, format string, medium string, payload string, box Box) ([]string, bool) {
	if !box.isDrawable() {
		return nil, false
	}

	imageID, isHeld := self.take(key, size)

	return place(imageID, isHeld, format, medium, payload, box)
}

func (self *Cache) take(key cacheKey, size Size) (int, bool) {
	if self == nil {
		return nextImageID(), false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	imageID, isKnown := self.imageIDs[key]
	if !isKnown {
		imageID = nextImageID()
		self.imageIDs[key] = imageID
		self.pictures[imageID] = &terminalPicture{bytes: max(size.Width*size.Height*bytesPerPixel, 0)}
	}

	self.placements++
	picture := self.pictures[imageID]
	picture.lastUse = self.placements

	return imageID, picture.isHeld
}

func (self *Cache) release(transmissions map[int]bool) string {
	var deletions strings.Builder

	for self.heldBytes > cacheBudget {
		imageID, isFound := self.leastRecentlyUsed(transmissions)
		if !isFound {
			break
		}

		picture := self.pictures[imageID]
		picture.isHeld = false
		self.heldBytes -= picture.bytes
		deletions.WriteString(deletion(imageID))
	}

	return deletions.String()
}

func (self *Cache) leastRecentlyUsed(transmissions map[int]bool) (int, bool) {
	oldestID := 0
	oldestUse := 0
	isFound := false

	for imageID, picture := range self.pictures {
		if !picture.isHeld || transmissions[imageID] {
			continue
		}
		if !isFound || picture.lastUse < oldestUse || (picture.lastUse == oldestUse && imageID < oldestID) {
			oldestID, oldestUse, isFound = imageID, picture.lastUse, true
		}
	}

	return oldestID, isFound
}

func transmittedID(command string) int {
	end := strings.IndexAny(command, ";\x1b")
	if end < 0 {
		end = len(command)
	}

	for field := range strings.SplitSeq(command[:end], ",") {
		if value, isIdentifier := strings.CutPrefix(field, identifierField); isIdentifier {
			imageID, err := strconv.Atoi(value)
			if err == nil {
				return imageID
			}
		}
	}

	return 0
}

func deletion(imageID int) string {
	return openCommand + "a=d,d=I,q=2,i=" + strconv.Itoa(imageID) + closeCommand
}
