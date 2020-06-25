package main_test

import (
	"testing"

	"github.com/intel/cdi/pkg/cdi-driver"
	"github.com/intel/cdi/pkg/coverage"
)

func TestMain(t *testing.T) {
	coverage.Run(cdidriver.Main)
}
