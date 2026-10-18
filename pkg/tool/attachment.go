package tool

type Attachment struct {
	Name      string
	MediaType string
	Data      []byte
}

func (self Attachment) IsPresent() bool {
	return self.MediaType != "" && len(self.Data) > 0
}
