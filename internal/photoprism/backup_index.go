package photoprism

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dustin/go-humanize/english"

	"github.com/photoprism/photoprism/internal/config"
	"github.com/photoprism/photoprism/pkg/clean"
	"github.com/photoprism/photoprism/pkg/fs"
)

// BackupIndex creates an SQL backup dump with the specified file and path name.
func BackupIndex(backupPath, fileName string, toStdOut, force bool, retain int) (err error) {
	c := Config()

	if !toStdOut {
		if backupPath == "" {
			backupPath = filepath.Join(c.BackupPath(), c.DatabaseDriver())
		}

		// Create the backup path if it does not already exist.
		if err = fs.MkdirAll(backupPath); err != nil {
			return err
		}

		// Check if the backup path is writable.
		if !fs.PathWritable(backupPath) {
			return fmt.Errorf("backup path is not writable")
		}

		if fileName == "" {
			backupFile := time.Now().UTC().Format("2006-01-02") + ".sql"
			fileName = filepath.Join(backupPath, backupFile)
		}

		if _, err = os.Stat(fileName); err == nil && !force {
			return fmt.Errorf("%s already exists", clean.Log(filepath.Base(fileName)))
		} else if err == nil {
			log.Warnf("replacing existing index backup")
		}

		// Create backup path if not exists.
		if dir := filepath.Dir(fileName); dir != "." {
			if err = fs.MkdirAll(dir); err != nil {
				return err
			}
		}
	}

	var cmd *exec.Cmd

	switch c.DatabaseDriver() {
	case config.MySQL, config.MariaDB:
		cmd = exec.Command(
			c.MariadbDumpBin(),
			"--protocol", "tcp",
			"-h", c.DatabaseHost(),
			"-P", c.DatabasePortString(),
			"-u", c.DatabaseUser(),
			"-p"+c.DatabasePassword(),
			c.DatabaseName(),
		)
	case config.SQLite3:
		if !fs.FileExistsNotEmpty(c.DatabaseFile()) {
			return fmt.Errorf("sqlite database %s not found", clean.LogQuote(c.DatabaseFile()))
		}

		cmd = exec.Command(
			c.SqliteBin(),
			c.DatabaseFile(),
			".dump",
		)
	default:
		return fmt.Errorf("unsupported database type: %s", c.DatabaseDriver())
	}

	// Write to stdout or file.
	var f *os.File
	if toStdOut {
		log.Infof("writing index backup to stdout")
		f = os.Stdout
	} else if f, err = os.OpenFile(fileName, os.O_TRUNC|os.O_RDWR|os.O_CREATE, fs.ModeFile); err != nil {
		return fmt.Errorf("failed to create %s: %s", clean.Log(fileName), err)
	} else {
		log.Infof("creating index backup in %s", clean.Log(filepath.Base(fileName)))
		defer f.Close()
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = f

	// Log exact command for debugging in trace mode.
	log.Trace(cmd.String())

	// Run backup command.
	if cmdErr := cmd.Run(); cmdErr != nil {
		if errStr := strings.TrimSpace(stderr.String()); errStr != "" {
			return errors.New(errStr)
		}

		return cmdErr
	}

	// Delete old backups if the number of backup files to keep has been specified.
	if !toStdOut && backupPath != "" && retain > 0 {
		files, globErr := filepath.Glob(filepath.Join(regexp.QuoteMeta(backupPath), SqlBackupFileNamePattern))

		if globErr != nil {
			return globErr
		}

		if len(files) == 0 {
			return fmt.Errorf("found no index backups files in %s", backupPath)
		} else if len(files) <= retain {
			return nil
		}

		sort.Strings(files)

		log.Infof("retaining %s", english.Plural(retain, "index backup", "index backups"))

		for i := 0; i < len(files)-retain; i++ {
			if err = os.Remove(files[i]); err != nil {
				return err
			} else {
				log.Infof("removed old backup file %s", clean.Log(filepath.Base(files[i])))
			}
		}
	}

	return nil
}