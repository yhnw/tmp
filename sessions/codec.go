package sessions

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
)

type Codec[T any] interface {
	Encode(*T) ([]byte, error)
	Decode([]byte) (*T, error)
}

type JSONCodec[T any] struct{}

func (JSONCodec[T]) Encode(session *T) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(session); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (JSONCodec[T]) Decode(data []byte) (*T, error) {
	var session T
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&session); err != nil {
		return nil, err
	}
	return &session, nil
}

type GobCodec[T any] struct{}

func (GobCodec[T]) Encode(data *T) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (GobCodec[T]) Decode(data []byte) (*T, error) {
	var session T
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&session); err != nil {
		return nil, err
	}
	return &session, nil
}
