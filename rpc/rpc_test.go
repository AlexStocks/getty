package rpc

import (
    "testing"
)

import (
    "github.com/stretchr/testify/assert"
)

func TestIsExported(t *testing.T) {
    exportedName := "Abc"
    notExportedName := "abc"
    flag := isExported(exportedName)
    assert.Equal(t, true, flag)
    flag = isExported(notExportedName)
    assert.Equal(t, false, flag)
}
