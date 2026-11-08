package internal

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
)

func zipSources(sources []string, target string, processBar *processbar.Model) error {
	var err error

	totalFiles := 0
	for _, src := range sources {
		if _, err = os.Stat(src); os.IsNotExist(err) {
			return fmt.Errorf("source path does not exist: %s", src)
		}
		count, e := countFiles(src)
		if e != nil {
			slog.Error("Error while zip file count files ", "error", e)
		}
		totalFiles += count
	}
	p, err := processBar.SendAddProcessMsg("zip file", totalFiles, true)
	if err != nil {
		return fmt.Errorf("cannot spawn process : %w", err)
	}
	_, err = os.Stat(target)
	if err == nil {
		p.Name = icon.CompressFile + icon.Space + "File already exist"
		p.State = processbar.Cancelled
		p.DoneTime = time.Now()
		pSendErr := processBar.SendUpdateProcessMsg(p, true)
		if pSendErr != nil {
			slog.Error("Error sending process update", "error", pSendErr)
		}
		return errors.New("file already exists")
	}

	f, err := os.Create(target)
	if err != nil {
		return err
	}
	defer f.Close()
	writer := zip.NewWriter(f)
	defer writer.Close()

	zipErr := zipSourcesCore(sources, processBar, &p, writer)

	if p.State != processbar.Failed {
		// TODO: User p.SetSuccessful(), p.SetFailed()
		p.State = processbar.Successful
		p.Done = totalFiles
	}
	p.DoneTime = time.Now()
	pSendErr := processBar.SendUpdateProcessMsg(p, true)
	if pSendErr != nil {
		slog.Error("Error sending process update", "error", pSendErr)
	}
	return zipErr
}

func zipSourcesCore(sources []string, processBar *processbar.Model,
	p *processbar.Process, writer *zip.Writer) error {
	var failures failureTally
	for _, src := range sources {
		srcParentDir := filepath.Dir(src)
		err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
			p.Name = icon.CompressFile + icon.Space + filepath.Base(path)
			if err == nil {
				err = zipEntry(srcParentDir, path, info, writer)
			}
			if err != nil {
				failures.add(err)
				return nil
			}

			p.Done++
			processBar.TrySendingUpdateProcessMsg(*p)
			return nil
		})
		if err != nil {
			slog.Error("Error while zip file", "error", err)
			p.State = processbar.Failed
			failures.add(err)
		}
	}

	if failures.count > 0 {
		p.State = processbar.Failed
		return fmt.Errorf("failed to zip %d entries: %w", failures.count, failures.first)
	}

	return nil
}

// zipEntry writes one entry of a walk into the archive, named by its path
// relative to base.
func zipEntry(base, path string, info os.FileInfo, writer *zip.Writer) error {
	relPath, err := filepath.Rel(base, path)
	if err != nil {
		return err
	}
	return writeZipFile(path, relPath, info, writer)
}

func writeZipFile(path string, relPath string, info os.FileInfo, writer *zip.Writer) error {
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Method = zip.Deflate
	header.Name = relPath
	if info.IsDir() {
		header.Name += "/"
	}
	headerWriter, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(headerWriter, file)
	if err != nil {
		return err
	}
	return nil
}

func getZipArchiveName(base string) (string, error) {
	zipName := strings.TrimSuffix(base, filepath.Ext(base)) + ".zip"
	zipName, err := renameIfDuplicate(zipName)
	return zipName, err
}
