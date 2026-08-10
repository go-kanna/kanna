package mapper_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/mapper"
)

func TestRegisterAndConvert(t *testing.T) {
	t.Parallel()

	type celsius float64
	type fahrenheit float64
	mapper.Register(func(c celsius) fahrenheit {
		return fahrenheit(c*9/5 + 32)
	})

	got, err := mapper.Convert[celsius, fahrenheit](100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 212 {
		t.Errorf("got %v, want 212", got)
	}
}

func TestRegisterEAndConvert(t *testing.T) {
	t.Parallel()

	type raw string
	type parsed int
	mapper.RegisterE(func(s raw) (parsed, error) {
		n, err := strconv.Atoi(string(s))
		return parsed(n), err
	})

	got, err := mapper.Convert[raw, parsed]("42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Errorf("got %v, want 42", got)
	}

	if _, err := mapper.Convert[raw, parsed]("not a number"); err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestConvertNotRegistered(t *testing.T) {
	t.Parallel()

	type unknownSrc struct{}
	type unknownDst struct{}
	_, err := mapper.Convert[unknownSrc, unknownDst](unknownSrc{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no converter registered") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	t.Parallel()

	type meters int
	type feet int
	conv := func(m meters) feet {
		return feet(float64(m) * 3.28)
	}
	mapper.Register(conv)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	mapper.Register(conv)
}

func TestRegisterNilPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil converter")
		}
	}()
	mapper.Register[bool, bool](nil)
}

func TestRegisterENilPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil converter")
		}
	}()
	mapper.RegisterE[bool, bool](nil)
}
