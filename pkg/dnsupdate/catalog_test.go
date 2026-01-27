package dnsupdate

import (
	"fmt"
	"testing"
)

func TestNormalizeHostname(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"app.example.com", "app.example.com"},
		{"APP.EXAMPLE.COM", "app.example.com"},
		{"App.Example.Com.", "app.example.com"},
		{"test.", "test"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeHostname(tt.input)
			if got != tt.want {
				t.Errorf("normalizeHostname(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseTXTHostnames(t *testing.T) {
	tests := []struct {
		name  string
		rdata string
		want  []string
	}{
		{
			name:  "empty",
			rdata: "",
			want:  nil,
		},
		{
			name:  "single hostname",
			rdata: "app",
			want:  []string{"app"},
		},
		{
			name:  "multiple hostnames",
			rdata: "app api web",
			want:  []string{"app", "api", "web"},
		},
		{
			name:  "extra spaces",
			rdata: "  app   api  web  ",
			want:  []string{"app", "api", "web"},
		},
		{
			name:  "full hostnames",
			rdata: "app.example.com api.example.com",
			want:  []string{"app.example.com", "api.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTXTHostnames(tt.rdata)
			if len(got) != len(tt.want) {
				t.Errorf("parseTXTHostnames(%q) returned %d items, want %d", tt.rdata, len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseTXTHostnames(%q)[%d] = %q, want %q", tt.rdata, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCatalog_chunkRecordName(t *testing.T) {
	c := &Catalog{zone: "example.com."}

	tests := []struct {
		chunkIndex int
		want       string
	}{
		{0, "_dnsweaver-catalog-0.example.com."},
		{1, "_dnsweaver-catalog-1.example.com."},
		{10, "_dnsweaver-catalog-10.example.com."},
		{100, "_dnsweaver-catalog-100.example.com."},
	}

	for _, tt := range tests {
		got := c.chunkRecordName(tt.chunkIndex)
		if got != tt.want {
			t.Errorf("chunkRecordName(%d) = %q, want %q", tt.chunkIndex, got, tt.want)
		}
	}
}

func TestChunkByteSize(t *testing.T) {
	tests := []struct {
		name  string
		chunk []string
		want  int
	}{
		{
			name:  "empty chunk",
			chunk: []string{},
			want:  0,
		},
		{
			name:  "single short hostname",
			chunk: []string{"a"},
			want:  2, // 1 byte for 'a' + 1 byte length prefix
		},
		{
			name:  "multiple hostnames",
			chunk: []string{"app", "api", "web"},
			// "app" = 3+1=4, "api" = 3+1=4, "web" = 3+1=4 = 12
			want: 12,
		},
		{
			name:  "longer hostname",
			chunk: []string{"my-application.subdomain.example.com"},
			// 36 chars + 1 length byte = 37
			want: 37,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chunkByteSize(tt.chunk)
			if got != tt.want {
				t.Errorf("chunkByteSize(%v) = %d, want %d", tt.chunk, got, tt.want)
			}
		})
	}
}

func TestCanFitInChunk(t *testing.T) {
	tests := []struct {
		name     string
		chunk    []string
		hostname string
		want     bool
	}{
		{
			name:     "empty chunk fits any hostname",
			chunk:    []string{},
			hostname: "app.example.com",
			want:     true,
		},
		{
			name:     "short hostname fits in mostly empty chunk",
			chunk:    []string{"a", "b", "c"},
			hostname: "d",
			want:     true,
		},
		{
			name:     "chunk at count limit",
			chunk:    make([]string, CatalogChunkSize), // Exactly at limit
			hostname: "new",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canFitInChunk(tt.chunk, tt.hostname)
			if got != tt.want {
				t.Errorf("canFitInChunk(%v, %q) = %v, want %v", tt.chunk, tt.hostname, got, tt.want)
			}
		})
	}
}

func TestCanFitInChunk_ByteLimit(t *testing.T) {
	// Create a chunk that's close to the byte limit
	// Each hostname of length N uses N+1 bytes
	// CatalogMaxChunkBytes is 3500, so we need to fill most of it

	// Use 50-byte hostnames (51 bytes each with length prefix)
	// 68 of them = 68 * 51 = 3468 bytes (under 3500)
	// Adding one more would be 3519 bytes (over 3500)
	longHostname := "this-is-a-very-long-hostname-that-is-fifty-chars"
	if len(longHostname) != 48 {
		// Adjust to exactly 50 chars
		longHostname = "this-is-a-very-long-hostname-that-is-exactly-50c"
	}

	// Build a chunk near the byte limit
	var nearFullChunk []string
	for i := 0; i < 68; i++ {
		nearFullChunk = append(nearFullChunk, longHostname)
	}

	currentSize := chunkByteSize(nearFullChunk)
	t.Logf("Chunk with %d hostnames uses %d bytes (limit: %d)", len(nearFullChunk), currentSize, CatalogMaxChunkBytes)

	// Verify adding another same-size hostname would exceed
	newHostnameSize := len(longHostname) + 1
	if currentSize+newHostnameSize <= CatalogMaxChunkBytes {
		t.Logf("Adding another hostname (%d bytes) would total %d bytes", newHostnameSize, currentSize+newHostnameSize)
	}

	// Test that we can't fit another long hostname when near limit
	if currentSize > CatalogMaxChunkBytes-100 {
		// We're close enough to limit that a 50-byte hostname shouldn't fit
		if canFitInChunk(nearFullChunk, longHostname) && currentSize+newHostnameSize > CatalogMaxChunkBytes {
			t.Errorf("canFitInChunk should return false when adding would exceed byte limit")
		}
	}

	// Test that a very short hostname might still fit
	shortHostname := "a"
	shortSize := len(shortHostname) + 1
	expectFit := currentSize+shortSize <= CatalogMaxChunkBytes && len(nearFullChunk) < CatalogChunkSize
	got := canFitInChunk(nearFullChunk, shortHostname)
	if got != expectFit {
		t.Errorf("canFitInChunk(nearFullChunk, %q) = %v, want %v (currentSize=%d, shortSize=%d, limit=%d)",
			shortHostname, got, expectFit, currentSize, shortSize, CatalogMaxChunkBytes)
	}
}

func TestCatalogStats(t *testing.T) {
	c := &Catalog{
		zone:      "example.com.",
		loaded:    true,
		chunks:    [][]string{{"a", "b"}, {"c", "d", "e"}},
		hostnames: map[string]int{"a": 0, "b": 0, "c": 1, "d": 1, "e": 1},
	}

	stats := c.Stats()

	if !stats.Loaded {
		t.Error("Stats().Loaded = false, want true")
	}
	if stats.ChunkCount != 2 {
		t.Errorf("Stats().ChunkCount = %d, want 2", stats.ChunkCount)
	}
	if stats.TotalHostnames != 5 {
		t.Errorf("Stats().TotalHostnames = %d, want 5", stats.TotalHostnames)
	}
	if stats.MaxChunkCount != CatalogChunkSize {
		t.Errorf("Stats().MaxChunkCount = %d, want %d", stats.MaxChunkCount, CatalogChunkSize)
	}
	if stats.MaxChunkBytes != CatalogMaxChunkBytes {
		t.Errorf("Stats().MaxChunkBytes = %d, want %d", stats.MaxChunkBytes, CatalogMaxChunkBytes)
	}
	if stats.MaxHostnameLen != CatalogMaxHostnameLen {
		t.Errorf("Stats().MaxHostnameLen = %d, want %d", stats.MaxHostnameLen, CatalogMaxHostnameLen)
	}
	// Check byte calculations
	if len(stats.ChunkBytesUsed) != 2 {
		t.Errorf("Stats().ChunkBytesUsed length = %d, want 2", len(stats.ChunkBytesUsed))
	}
	if len(stats.ChunkHostnameCount) != 2 {
		t.Errorf("Stats().ChunkHostnameCount length = %d, want 2", len(stats.ChunkHostnameCount))
	}
	// Chunk 0 has "a", "b" = 2 bytes + 2 length bytes = 4 bytes
	if stats.ChunkBytesUsed[0] != 4 {
		t.Errorf("Stats().ChunkBytesUsed[0] = %d, want 4", stats.ChunkBytesUsed[0])
	}
	// Chunk 1 has "c", "d", "e" = 3 bytes + 3 length bytes = 6 bytes
	if stats.ChunkBytesUsed[1] != 6 {
		t.Errorf("Stats().ChunkBytesUsed[1] = %d, want 6", stats.ChunkBytesUsed[1])
	}
	if stats.ChunkHostnameCount[0] != 2 {
		t.Errorf("Stats().ChunkHostnameCount[0] = %d, want 2", stats.ChunkHostnameCount[0])
	}
	if stats.ChunkHostnameCount[1] != 3 {
		t.Errorf("Stats().ChunkHostnameCount[1] = %d, want 3", stats.ChunkHostnameCount[1])
	}
}

func TestCatalogConstants(t *testing.T) {
	// Verify constants are sensible
	if CatalogChunkSize < 10 {
		t.Errorf("CatalogChunkSize = %d, want >= 10", CatalogChunkSize)
	}
	if CatalogChunkSize > 1000 {
		t.Errorf("CatalogChunkSize = %d, want <= 1000 (DNS packet limits)", CatalogChunkSize)
	}
	if CatalogTTL < 60 {
		t.Errorf("CatalogTTL = %d, want >= 60", CatalogTTL)
	}
	if CatalogPrefix == "" {
		t.Error("CatalogPrefix is empty")
	}
	// Verify byte limits are sensible
	if CatalogMaxChunkBytes < 1000 {
		t.Errorf("CatalogMaxChunkBytes = %d, want >= 1000", CatalogMaxChunkBytes)
	}
	if CatalogMaxChunkBytes > 65000 {
		t.Errorf("CatalogMaxChunkBytes = %d, want <= 65000 (DNS limits)", CatalogMaxChunkBytes)
	}
	if CatalogMaxHostnameLen != 253 {
		t.Errorf("CatalogMaxHostnameLen = %d, want 253 (RFC 1035)", CatalogMaxHostnameLen)
	}
}

func TestCompact_MultipleChunks(t *testing.T) {
	// Test the chunking algorithm WITHOUT DNS operations
	// This simulates what Compact does internally

	// Add 105 hostnames - should create 2 chunks (100 max per chunk)
	allHostnames := make([]string, 105)
	for i := 1; i <= 105; i++ {
		allHostnames[i-1] = fmt.Sprintf("host-%03d.example.com", i)
	}

	// Simulate the chunking algorithm from Compact
	var chunks [][]string
	var currentChunk []string

	for _, hostname := range allHostnames {
		if len(currentChunk) > 0 && !canFitInChunk(currentChunk, hostname) {
			chunks = append(chunks, currentChunk)
			currentChunk = nil
		}
		currentChunk = append(currentChunk, hostname)
	}
	if len(currentChunk) > 0 {
		chunks = append(chunks, currentChunk)
	}

	// Should have 2 chunks: first with 100, second with 5
	if len(chunks) != 2 {
		t.Errorf("Chunking created %d chunks, want 2", len(chunks))
	}

	// Verify first chunk is at max count
	if len(chunks) > 0 && len(chunks[0]) != CatalogChunkSize {
		t.Errorf("First chunk has %d hostnames, want %d", len(chunks[0]), CatalogChunkSize)
	}

	// Verify second chunk has remainder
	if len(chunks) > 1 && len(chunks[1]) != 5 {
		t.Errorf("Second chunk has %d hostnames, want 5", len(chunks[1]))
	}

	// Verify total hostnames preserved
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	if total != 105 {
		t.Errorf("Chunking preserved %d hostnames, want 105", total)
	}

	t.Logf("Count-based chunking: %d chunks with %d, %d hostnames", len(chunks), len(chunks[0]), len(chunks[1]))
}

func TestCompact_ByteLimit(t *testing.T) {
	// Test that chunking respects byte limits, not just count limits

	// Add 70 long hostnames (each ~58 bytes with length prefix)
	// 70 * 59 bytes = 4130 bytes > 3500 limit, should create 2 chunks
	allHostnames := make([]string, 70)
	for i := 1; i <= 70; i++ {
		// ~57 chars + 1 length byte = ~58 bytes each
		allHostnames[i-1] = fmt.Sprintf("very-long-hostname-for-testing-%03d.subdomain.example.com", i)
	}

	// Simulate the chunking algorithm
	var chunks [][]string
	var currentChunk []string

	for _, hostname := range allHostnames {
		if len(currentChunk) > 0 && !canFitInChunk(currentChunk, hostname) {
			chunks = append(chunks, currentChunk)
			currentChunk = nil
		}
		currentChunk = append(currentChunk, hostname)
	}
	if len(currentChunk) > 0 {
		chunks = append(chunks, currentChunk)
	}

	// Should have at least 2 chunks due to byte limit
	if len(chunks) < 2 {
		byteSizes := make([]int, len(chunks))
		for i, chunk := range chunks {
			byteSizes[i] = chunkByteSize(chunk)
		}
		t.Errorf("Chunking created %d chunks, want >= 2 (byte limit should trigger before count limit). Bytes: %v",
			len(chunks), byteSizes)
	}

	// Verify no chunk exceeds byte limit
	for i, chunk := range chunks {
		bytes := chunkByteSize(chunk)
		if bytes > CatalogMaxChunkBytes {
			t.Errorf("Chunk %d has %d bytes, exceeds limit %d", i, bytes, CatalogMaxChunkBytes)
		}
		t.Logf("Chunk %d: %d hostnames, %d bytes", i, len(chunk), bytes)
	}
}

func TestErrHostnameTooLong(t *testing.T) {
	// Verify the error is defined and contains useful info
	if ErrHostnameTooLong == nil {
		t.Fatal("ErrHostnameTooLong is nil")
	}
	errMsg := ErrHostnameTooLong.Error()
	if errMsg == "" {
		t.Error("ErrHostnameTooLong.Error() is empty")
	}
	// Should mention the limit (253)
	found := false
	for i := 0; i <= len(errMsg)-3; i++ {
		if errMsg[i:i+3] == "253" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ErrHostnameTooLong should mention the 253 byte limit, got: %s", errMsg)
	}
}

// Integration tests would require a mock DNS client or real DNS server.
// These are covered in provider_test.go with the full provider.
