package turso

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// progressReader is a custom io.Reader that tracks progress of the upload
// and calls the onProgress callback with the progress of the upload.
type progressReader struct {
	reader     io.Reader
	totalSize  int64
	bytesRead  int64
	startTime  time.Time
	onProgress func(progressPct int, uploadedBytes int64, totalBytes int64, elapsedTime time.Duration, done bool)
	lastUpdate int // Last reported progress percentage. Initially -1 to ensure first update is always sent.
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.bytesRead += int64(n)
		progressPct := int(float64(pr.bytesRead) / float64(pr.totalSize) * 100)

		// Only call progress if we've made at least 1% progress or if we're done
		if progressPct > pr.lastUpdate || err == io.EOF {
			elapsedTime := time.Since(pr.startTime)
			pr.lastUpdate = progressPct
			pr.onProgress(progressPct, pr.bytesRead, pr.totalSize, elapsedTime, err == io.EOF)
		}
	}
	return n, err
}

type TursoServerClient struct {
	tenant string
	client *Client
}

func NewTursoServerClient(tenantHostname string, token string, cliVersion string, org string) (TursoServerClient, error) {
	baseURL, err := url.Parse(fmt.Sprintf("https://%s", tenantHostname))
	if err != nil {
		return TursoServerClient{}, fmt.Errorf("unable to create TursoServerClient: %v", err)
	}
	newClient := New(baseURL, token, cliVersion, org)

	return TursoServerClient{
		tenant: org,
		client: newClient,
	}, nil
}

// UploadFile uploads a database file to the Turso server.
// it assumes a SQLite file exists at 'filepath'.
// it streams the file to the server, and calls the onProgress callback with the progress of the upload.
func (i *TursoServerClient) UploadFile(filepath string, onUploadProgress func(progressPct int, uploadedBytes int64, totalBytes int64, elapsedTime time.Duration, done bool)) error {
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filepath, err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file stats for %s: %w", filepath, err)
	}

	totalSize := stat.Size()

	// Create progress tracking reader
	progressTracker := &progressReader{
		reader:     file,
		totalSize:  totalSize,
		onProgress: onUploadProgress,
		startTime:  time.Now(),
		lastUpdate: -1, // Ensure first update is always sent
	}

	// Send POST request with streaming body
	r, err := i.client.PostBinary("/v1/upload", progressTracker)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}
	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("upload failed with status code %d and error reading response: %v", r.StatusCode, err)
		}
		return fmt.Errorf("upload failed with status code %d: %s", r.StatusCode, string(body))
	}

	return nil
}
