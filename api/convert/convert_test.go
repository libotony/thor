package convert

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapSlice(t *testing.T) {
	require.Nil(t, MapSlice([]int(nil), strconv.Itoa))
	require.Equal(t, []string{"1", "2"}, MapSlice([]int{1, 2}, strconv.Itoa))
}
