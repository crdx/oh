package pictures

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util/imageutil"
	"crdx.org/io/internal/util/pathutil"

	"crdx.org/io/internal/app/graphics"
)

const maxPlacements = 32

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

type Display struct {
	SessionDirectory string
	ScratchDirectory string
	CellWidth        int
	CellHeight       int
	IsLocal          bool
}

type Drawer struct {
	cell    graphics.Size
	isLocal bool
	root    string
	scratch string

	mutex      sync.Mutex
	placements map[placementKey][]string
}

type placementKey struct {
	path       string
	columns    int
	sizeBytes  int64
	modifiedAt int64
}

type source struct {
	path     string
	data     []byte
	size     graphics.Size
	isByPath bool
}

func NewDrawer(display Display, root string) *Drawer {
	return &Drawer{
		cell:       graphics.Size{Width: display.CellWidth, Height: display.CellHeight},
		isLocal:    display.IsLocal,
		root:       root,
		scratch:    display.ScratchDirectory,
		placements: map[placementKey][]string{},
	}
}

func (self *Drawer) DrawPicture(path string, columns int) ([]string, bool) {
	resolvedPath, sizeBytes, modifiedAt, isFile := self.locate(path)
	if !isFile {
		return nil, false
	}

	key := placementKey{
		path:       resolvedPath,
		columns:    columns,
		sizeBytes:  sizeBytes,
		modifiedAt: modifiedAt,
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if rows, wasPlaced := self.placements[key]; wasPlaced {
		return rows, len(rows) > 0
	}

	rows, isPlaced := self.place(resolvedPath, columns)

	if len(self.placements) >= maxPlacements {
		clear(self.placements)
	}
	self.placements[key] = rows

	return rows, isPlaced
}

func (self *Drawer) place(path string, columns int) ([]string, bool) {
	picture, isOpened := self.open(path)
	if !isOpened {
		return nil, false
	}

	box, isDrawable := graphics.Fit(picture.size, self.cell, columns)
	if !isDrawable {
		return nil, false
	}

	if picture.isByPath {
		return graphics.PlaceFile(picture.path, box)
	}

	return graphics.PlacePNG(picture.data, box)
}

func (self *Drawer) open(path string) (source, bool) {
	//nolint:gosec // a path the model named in its own answer, already measured as a regular file
	data, err := os.ReadFile(path)
	if err != nil {
		return source{}, false
	}

	width, height, isMeasured := imageutil.Dimensions(data)
	if !isMeasured {
		return source{}, false
	}

	if self.isLocal && bytes.HasPrefix(data, pngSignature) {
		return source{
			path:     path,
			size:     graphics.Size{Width: width, Height: height},
			isByPath: true,
		}, true
	}

	displayCopy, displayWidth, displayHeight, err := shrink(data)
	if err != nil {
		return source{}, false
	}

	return source{
		data: displayCopy,
		size: graphics.Size{Width: displayWidth, Height: displayHeight},
	}, true
}

func (self *Drawer) underScratch(names []string) []string {
	var scratchPaths []string

	for _, name := range names {
		if beneath, isBeneath := pathutil.RelativeTo(sandbox.TmpDir, name); isBeneath {
			scratchPaths = append(scratchPaths, filepath.Join(self.scratch, beneath))
		}
	}

	return scratchPaths
}

func (self *Drawer) locate(path string) (string, int64, int64, bool) {
	for _, candidate := range self.candidates(path) {
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxStoredBytes {
			continue
		}

		return candidate, info.Size(), info.ModTime().UnixNano(), true
	}

	return "", 0, 0, false
}

func (self *Drawer) candidates(path string) []string {
	if path == "" || strings.Contains(path, "://") {
		return nil
	}

	names := []string{path}

	if plainPath, err := url.PathUnescape(path); err == nil && plainPath != path {
		names = append(names, plainPath)
	}

	if self.scratch != "" {
		if scratchPaths := self.underScratch(names); scratchPaths != nil {
			return scratchPaths
		}
	}

	if filepath.IsAbs(path) || self.root == "" {
		return names
	}

	rootedPaths := make([]string, 0, len(names))
	for _, name := range names {
		rootedPaths = append(rootedPaths, filepath.Join(self.root, name))
	}

	return rootedPaths
}
