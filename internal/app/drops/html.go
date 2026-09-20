package drops

const (
	htmlName      = "fetch"
	htmlExtension = ".html"
)

func SaveHTML(sessionDirectory string, ensureSession func() error, contents []byte) (string, error) {
	return writeDrop(sessionDirectory, ensureSession, htmlName, htmlExtension, contents)
}
