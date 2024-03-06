package formatter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"

	"gitlab.com/gorib/pry/option"
)

func Json() io.Writer {
	zerolog.TimestampFieldName = "@timestamp"
	return jsonFormatter{FieldsExclude: option.DefaultExcludedFields, Out: os.Stdout}
}

type jsonFormatter struct {
	FieldsExclude []string
	Out           io.Writer
}

func (j jsonFormatter) Write(p []byte) (n int, err error) {
	var evt map[string]interface{}
	d := json.NewDecoder(bytes.NewReader(p))
	d.UseNumber()
	err = d.Decode(&evt)
	if err != nil {
		return n, fmt.Errorf("cannot decode event: %s", err)
	}

	for _, exclude := range j.FieldsExclude {
		delete(evt, exclude)
	}

	b, err := json.Marshal(evt)
	if err != nil {
		return n, fmt.Errorf("cannot encode event: %s", err)
	}
	b = append(b, '\n')

	n, err = j.Out.Write(b)
	if err != nil {
		return n, err
	}
	if n != len(b) {
		return n, io.ErrShortWrite
	}
	return len(p), nil
}
