package drops

const (
	outputName      = "output"
	outputExtension = ".txt"
)

func SaveOutput(sessionDirectory string, ensureSession func() error, output string) (string, error) {
	return writeDrop(sessionDirectory, ensureSession, outputName, outputExtension, []byte(output))
}
