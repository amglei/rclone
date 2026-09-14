// Test metadata mapping from SFTP stat info

package sftp

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/rclone/rclone/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFileInfo implements os.FileInfo with a configurable Sys
type fakeFileInfo struct {
	name string
	size int64
	mode os.FileMode
	sys  any
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return f.sys }

func TestMetadataFromFileStat(t *testing.T) {
	mtime := time.Unix(1735689600, 0)
	atime := time.Unix(1735689500, 0)
	info := fakeFileInfo{
		name: "file.bin",
		size: 1234,
		sys: &sftp.FileStat{
			Size:  1234,
			Mode:  0o100644,
			UID:   1000,
			GID:   1000,
			Mtime: uint32(mtime.Unix()),
			Atime: uint32(atime.Unix()),
		},
	}
	metadata := metadataFromFileStat(info)
	require.NotNil(t, metadata)

	mode, err := strconv.ParseInt(metadata["mode"], 8, 64)
	require.NoError(t, err)
	assert.Equal(t, 0o100644, int(mode))
	assert.Equal(t, "1000", metadata["uid"])
	assert.Equal(t, "1000", metadata["gid"])

	parsedMtime, err := time.Parse(time.RFC3339Nano, metadata["mtime"])
	require.NoError(t, err)
	assert.Equal(t, mtime.Unix(), parsedMtime.Unix())

	parsedAtime, err := time.Parse(time.RFC3339Nano, metadata["atime"])
	require.NoError(t, err)
	assert.Equal(t, atime.Unix(), parsedAtime.Unix())

	// SFTP has no ctime/btime
	assert.NotContains(t, metadata, "ctime")
	assert.NotContains(t, metadata, "btime")

	// No SFTP stat info -> no metadata
	assert.Nil(t, metadataFromFileStat(fakeFileInfo{sys: nil}))
}

func TestDirectoryMetadata(t *testing.T) {
	mtime := time.Unix(1735689600, 0)
	info := fakeFileInfo{
		name: "dir",
		mode: os.ModeDir | 0o755,
		sys: &sftp.FileStat{
			Mode:  0o040755,
			UID:   500,
			GID:   500,
			Mtime: uint32(mtime.Unix()),
			Atime: uint32(mtime.Unix()),
		},
	}
	d := &Directory{
		Dir:  fs.NewDir("dir", mtime),
		info: info,
	}
	metadata, err := d.Metadata(t.Context())
	require.NoError(t, err)
	require.NotNil(t, metadata)
	assert.Equal(t, "40755", metadata["mode"])
	assert.Equal(t, "500", metadata["uid"])
	assert.Equal(t, "500", metadata["gid"])

	parsedMtime, err := time.Parse(time.RFC3339Nano, metadata["mtime"])
	require.NoError(t, err)
	assert.Equal(t, mtime.Unix(), parsedMtime.Unix())

	// the embedded Directory still works
	assert.Equal(t, "dir", d.Remote())
	assert.Equal(t, mtime.Unix(), d.ModTime(t.Context()).Unix())
}
