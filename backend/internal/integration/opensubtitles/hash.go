package opensubtitles

import (
	"encoding/binary"
	"fmt"
	"os"
)

const hashChunkSize = 65536 // 64KB

// ComputeHash calculates the OpenSubtitles file hash for the given path.
// Algorithm: sum of uint64 LE values from first + last 64KB, plus file size.
// Returns a 16-digit zero-padded hexadecimal string.
func ComputeHash(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening file for hash: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file for hash: %w", err)
	}

	size := fi.Size()
	if size < int64(hashChunkSize*2) {
		return "", fmt.Errorf("file too small for hash (%d bytes)", size)
	}

	var hash uint64 = uint64(size)

	// Read first 64KB
	buf := make([]byte, hashChunkSize)
	if _, err := f.Read(buf); err != nil {
		return "", fmt.Errorf("reading first chunk: %w", err)
	}
	hash = sumChunk(buf, hash)

	// Read last 64KB
	if _, err := f.Seek(-hashChunkSize, 2); err != nil {
		return "", fmt.Errorf("seeking to last chunk: %w", err)
	}
	if _, err := f.Read(buf); err != nil {
		return "", fmt.Errorf("reading last chunk: %w", err)
	}
	hash = sumChunk(buf, hash)

	return fmt.Sprintf("%016x", hash), nil
}

func sumChunk(buf []byte, hash uint64) uint64 {
	for i := 0; i+8 <= len(buf); i += 8 {
		hash += binary.LittleEndian.Uint64(buf[i : i+8])
	}
	return hash
}
