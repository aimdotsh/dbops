package agentclient

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var transferIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{16,80}$`)

// Chunks traverse the existing authenticated Agent connection. They must never
// be persisted as task events (the server dispatches these with TaskID zero).
func backupTransfer(ctx context.Context, work, action string, p map[string]any) (map[string]any, error) {
	id, _ := p["transfer_id"].(string)
	if !transferIDPattern.MatchString(id) {
		return nil, errors.New("invalid transfer id")
	}
	root := filepath.Join(work, "transfers", id)
	archive := filepath.Join(root, "backup.tgz")
	if action == "backup.transfer.cleanup" {
		return nil, os.RemoveAll(root)
	}
	switch action {
	case "backup.transfer.export":
		source, _ := p["backup_path"].(string)
		engine, _ := p["engine"].(string)
		expected, _ := p["sha256"].(string)
		if !filepath.IsAbs(source) || len(expected) != 64 {
			return nil, errors.New("backup path and checksum required")
		}
		if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
			return nil, err
		}
		if err := os.Mkdir(root, 0700); err != nil {
			return nil, err
		}
		snapshot := filepath.Join(root, "snapshot")
		if engine == "xtrabackup" {
			if err := copyPhysicalBackup(ctx, source, snapshot); err != nil {
				return nil, err
			}
			_, sum, err := physicalBackupDigest(ctx, snapshot)
			if err != nil {
				return nil, err
			}
			if sum != expected {
				return nil, errors.New("physical backup checksum mismatch")
			}
		} else if engine == "mysqldump" {
			if err := os.Mkdir(snapshot, 0700); err != nil {
				return nil, err
			}
			info, err := os.Lstat(source)
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, errors.New("backup must be regular")
			}
			in, err := os.Open(source)
			if err != nil {
				return nil, err
			}
			defer in.Close()
			out, err := os.OpenFile(filepath.Join(snapshot, "backup.sql.gz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return nil, err
			}
			_, err = io.Copy(out, in)
			ce := out.Close()
			if err != nil {
				return nil, err
			}
			if ce != nil {
				return nil, ce
			}
			sum, err := fileSHA256(filepath.Join(snapshot, "backup.sql.gz"))
			if err != nil {
				return nil, err
			}
			if sum != expected {
				return nil, errors.New("logical backup checksum mismatch")
			}
		} else {
			return nil, errors.New("unsupported backup engine")
		}
		if err := packBackup(ctx, snapshot, archive); err != nil {
			return nil, err
		}
		sum, err := fileSHA256(archive)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(archive)
		if err != nil {
			return nil, err
		}
		return map[string]any{"sha256": sum, "size_bytes": info.Size()}, nil
	case "backup.transfer.read":
		offset, err := intParam(p, "offset")
		if err != nil || offset < 0 {
			return nil, errors.New("invalid offset")
		}
		f, err := os.Open(archive)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b := make([]byte, 256<<10)
		n, err := f.ReadAt(b, int64(offset))
		if err != nil && err != io.EOF {
			return nil, err
		}
		return map[string]any{"chunk": base64.StdEncoding.EncodeToString(b[:n]), "bytes": n}, nil
	case "backup.transfer.write":
		offset, err := intParam(p, "offset")
		if err != nil || offset < 0 {
			return nil, errors.New("invalid offset")
		}
		encoded, _ := p["chunk"].(string)
		if len(encoded) > 400000 {
			return nil, errors.New("chunk too large")
		}
		b, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, err
		}
		if offset == 0 {
			if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
				return nil, err
			}
			if err := os.Mkdir(root, 0700); err != nil {
				return nil, err
			}
		}
		f, err := os.OpenFile(archive, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if info.Size() != int64(offset) {
			return nil, errors.New("non-sequential transfer")
		}
		n, err := f.WriteAt(b, int64(offset))
		return map[string]any{"bytes": n}, err
	case "backup.transfer.finish":
		expected, _ := p["sha256"].(string)
		sum, err := fileSHA256(archive)
		if err != nil {
			return nil, err
		}
		if len(expected) != 64 || sum != expected {
			return nil, errors.New("transfer checksum mismatch")
		}
		dest := filepath.Join(root, "snapshot")
		if err := os.Mkdir(dest, 0700); err != nil {
			return nil, err
		}
		if err := unpackBackup(archive, dest); err != nil {
			return nil, err
		}
		return map[string]any{"path": dest}, nil
	}
	return nil, errors.New("unsupported transfer operation")
}

func packBackup(ctx context.Context, source, archive string) error {
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == source {
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("non-regular backup file")
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		h.Mode = 0600
		if info.IsDir() {
			h.Mode = 0700
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(tw, in)
		return err
	})
	te := tw.Close()
	ge := gz.Close()
	if err != nil {
		return err
	}
	if te != nil {
		return te
	}
	return ge
}

func unpackBackup(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if !filepath.IsLocal(h.Name) {
			return fmt.Errorf("unsafe archive name %q", h.Name)
		}
		path := filepath.Join(dest, h.Name)
		if h.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(path, 0700); err != nil {
				return err
			}
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return errors.New("archive links and special files are forbidden")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		ce := out.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
	}
}
