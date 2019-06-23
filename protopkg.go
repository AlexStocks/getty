package getty

import (
	"bytes"
)

type ProtoPackage interface {
	Marshal() (*bytes.Buffer, error)
	Unmarshal(buf *bytes.Buffer) (int, error)
}
