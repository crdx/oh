package graphics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var smallBox = Box{Cells: 4, Rows: 2}

func transmits(rows []string) bool {
	return strings.Contains(strings.Join(rows, ""), transmissionStart)
}

func placedID(t *testing.T, rows []string) int {
	t.Helper()

	colourAt := strings.Index(rows[len(rows)-1], "\x1b[38;2;")
	if colourAt < 0 {
		t.Fatalf("no identifying colour in %q", rows[len(rows)-1])
	}

	var red, green, blue int
	if _, err := fmt.Sscanf(rows[len(rows)-1][colourAt:], "\x1b[38;2;%d;%d;%dm", &red, &green, &blue); err != nil {
		t.Fatal(err)
	}

	return red<<16 | green<<8 | blue
}

func writeRows(cache *Cache, rows []string) string {
	text := strings.Join(rows, "\n")
	return cache.Observe(text)
}

func TestAPictureTheTerminalHoldsIsPlacedWithoutBeingSentAgain(t *testing.T) {
	cache := NewCache()

	first, _ := cache.PlacePNG([]byte("\x89PNG one"), Size{Width: 40, Height: 40}, smallBox)
	writeRows(cache, first)
	second, _ := cache.PlacePNG([]byte("\x89PNG one"), Size{Width: 40, Height: 40}, smallBox)

	if !transmits(first) {
		t.Error("the first placement did not send the picture")
	}
	if transmits(second) {
		t.Error("a picture the terminal holds was sent again")
	}
	if placedID(t, first) != placedID(t, second) {
		t.Error("the second placement names a different picture")
	}
}

func TestAPictureWhoseTransmissionWasNeverWrittenIsSentAgain(t *testing.T) {
	cache := NewCache()

	first, _ := cache.PlacePNG([]byte("\x89PNG one"), Size{Width: 40, Height: 40}, smallBox)
	second, _ := cache.PlacePNG([]byte("\x89PNG one"), Size{Width: 40, Height: 40}, smallBox)

	if !transmits(second) {
		t.Error("a discarded transmission was taken as held")
	}
	if placedID(t, first) != placedID(t, second) {
		t.Error("sending the picture again chose a new identifier")
	}
}

func TestEachBoxIsAPictureOfItsOwn(t *testing.T) {
	cache := NewCache()

	wide, _ := cache.PlacePNG([]byte("\x89PNG one"), Size{Width: 40, Height: 40}, smallBox)
	writeRows(cache, wide)
	narrow, _ := cache.PlacePNG([]byte("\x89PNG one"), Size{Width: 40, Height: 40}, Box{Cells: 2, Rows: 1})

	if !transmits(narrow) {
		t.Error("a picture in a new box was not sent")
	}
	if placedID(t, wide) == placedID(t, narrow) {
		t.Error("two boxes share one picture, so one draws at the other's shape")
	}
}

func TestAFileChangedOnDiskIsSentAfresh(t *testing.T) {
	cache := NewCache()
	path := filepath.Join(t.TempDir(), "chart.png")
	if err := os.WriteFile(path, []byte("\x89PNG one"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, _ := cache.PlaceFile(path, Size{Width: 40, Height: 40}, smallBox)
	writeRows(cache, first)

	if err := os.WriteFile(path, []byte("\x89PNG two, longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	second, _ := cache.PlaceFile(path, Size{Width: 40, Height: 40}, smallBox)

	if !transmits(second) {
		t.Error("a changed file was drawn from what the terminal held before")
	}
}

func TestAFileThatCannotBeFoundIsStillSent(t *testing.T) {
	rows, isPlaced := NewCache().PlaceFile(filepath.Join(t.TempDir(), "gone.png"), Size{}, smallBox)

	if !isPlaced || !transmits(rows) {
		t.Error("a missing file was not left to the terminal to refuse")
	}
}

func TestWithoutACacheEveryPictureIsSent(t *testing.T) {
	var cache *Cache

	first, _ := cache.PlacePNG([]byte("\x89PNG one"), Size{}, smallBox)
	if deletions := writeRows(cache, first); deletions != "" {
		t.Errorf("no cache deleted %q", deletions)
	}
	second, _ := cache.PlacePNG([]byte("\x89PNG one"), Size{}, smallBox)

	if !transmits(second) {
		t.Error("a picture was taken as held with nothing to hold it")
	}
}

func TestAPictureSomethingElsePlacedIsIgnored(t *testing.T) {
	cache := NewCache()

	if deletions := cache.Observe(openCommand + "a=T,f=32,i=999999,m=0;AAAA" + closeCommand); deletions != "" {
		t.Errorf("a stranger's picture caused deletions %q", deletions)
	}
	if cache.heldBytes != 0 {
		t.Errorf("a stranger's picture counts %d bytes", cache.heldBytes)
	}
}

func TestPicturesBeyondTheBudgetAreDeletedLeastRecentlyUsedFirst(t *testing.T) {
	cache := NewCache()
	huge := Size{Width: 5120, Height: 5120}

	oldest, _ := cache.PlacePNG([]byte("\x89PNG one"), huge, smallBox)
	writeRows(cache, oldest)
	middle, _ := cache.PlacePNG([]byte("\x89PNG two"), huge, smallBox)
	writeRows(cache, middle)
	again, _ := cache.PlacePNG([]byte("\x89PNG one"), huge, smallBox)
	writeRows(cache, again)
	newest, _ := cache.PlacePNG([]byte("\x89PNG three"), huge, smallBox)
	deletions := writeRows(cache, newest)

	if want := deletion(placedID(t, middle)); deletions != want {
		t.Errorf("deleted %q, want %q", deletions, want)
	}
	if cache.heldBytes > cacheBudget {
		t.Errorf("holds %d bytes, beyond the budget of %d", cache.heldBytes, cacheBudget)
	}

	afterDeletion, _ := cache.PlacePNG([]byte("\x89PNG two"), huge, smallBox)
	if !transmits(afterDeletion) {
		t.Error("a deleted picture was taken as still held")
	}
	if placedID(t, afterDeletion) != placedID(t, middle) {
		t.Error("a deleted picture came back under a new identifier")
	}
}

func TestAPictureLargerThanTheBudgetIsNeverDeletedAsItIsSent(t *testing.T) {
	cache := NewCache()

	rows, _ := cache.PlacePNG([]byte("\x89PNG one"), Size{Width: 9000, Height: 9000}, smallBox)
	if deletions := writeRows(cache, rows); deletions != "" {
		t.Errorf("deleted %q in the same breath as sending it", deletions)
	}
}

func TestATransmissionIsReadWhereverItsIdentifierStands(t *testing.T) {
	for name, command := range map[string]string{
		"middle": "U=1,q=2,f=100,t=f,i=42,c=4,r=2,m=0;AAAA",
		"last":   "U=1,i=42;AAAA",
		"bare":   "i=42",
	} {
		t.Run(name, func(t *testing.T) {
			if got := transmittedID(command); got != 42 {
				t.Errorf("read %d, want 42", got)
			}
		})
	}
}
