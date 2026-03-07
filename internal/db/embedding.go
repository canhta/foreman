package db

import (
	"encoding/binary"
	"math"
)

// EmbeddingRecord represents one chunk of a source file with its embedding vector.
type EmbeddingRecord struct {
	RepoPath  string
	HeadSHA   string
	FilePath  string
	ChunkText string
	Vector    []float32
	StartLine int
	EndLine   int
}

// SerializeVector converts []float32 to little-endian bytes for BLOB storage.
func SerializeVector(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DeserializeVector converts little-endian bytes back to []float32.
// Returns nil if b is nil or has a length that is not a multiple of 4.
func DeserializeVector(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// CosineSimilarity computes the cosine similarity between two equal-length vectors.
// Returns 0 if either vector is zero-length or lengths differ.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}
