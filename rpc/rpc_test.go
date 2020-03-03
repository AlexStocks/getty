package rpc

import (
    "github.com/stretchr/testify/assert"
    "testing"
)

func TestIsExported(t *testing.T) {
    exportedName := "Abc"
    notExportedName := "abc"
    flag := isExported(exportedName)
    assert.Equal(t, true, flag)
    flag = isExported(notExportedName)
    assert.Equal(t, false, flag)
}
