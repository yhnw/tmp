package sessions

import "testing"

func TestJSONCodec(t *testing.T) {
	want := testSession{N: 42}
	codec := JSONCodec[testSession]{}
	b, err := codec.Encode(&want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if *got != want {
		t.Fatalf("got %+v; want %+v", *got, want)
	}
}

func TestGobCodec(t *testing.T) {
	want := testSession{N: 42}
	codec := GobCodec[testSession]{}
	b, err := codec.Encode(&want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if *got != want {
		t.Fatalf("got %+v; want %+v", *got, want)
	}
}
