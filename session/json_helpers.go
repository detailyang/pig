package session

import (
	"bytes"
	"encoding/json"
)

func marshalJSONNoHTMLEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func marshalJSONIndentNoHTMLEscape(value any, prefix string, indent string) ([]byte, error) {
	data, err := marshalJSONNoHTMLEscape(value)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, data, prefix, indent); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
