package workerstatus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
)

func WriteDocument(ctx context.Context, path string, document Document) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if path == "" {
		return errors.New("worker status path is required")
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode worker status: %w", err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create worker status directory: %w", err)
	}
	temporary := fmt.Sprintf("%s.%d.%s.tmp", path, os.Getpid(), uuid.NewString())
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create worker status file: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporary)
		}
	}()
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write worker status file: %w", err)
	}
	if err := config.ProtectSensitiveFile(temporary); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := replaceFile(temporary, path); err != nil {
		return fmt.Errorf("replace worker status file: %w", err)
	}
	removeTemporary = false
	return nil
}
