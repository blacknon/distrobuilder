package sources

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/lxc/distrobuilder/shared"
)

type vyos struct {
	common

	fname string
	fpath string
}

func (s *vyos) Run() error {
	err := s.downloadImage(s.definition)
	if err != nil {
		return fmt.Errorf("Failed to download image: %w", err)
	}

	return s.unpackISO(filepath.Join(s.fpath, s.fname), s.rootfsDir)
}

func (s *vyos) downloadImage(definition shared.Definition) error {
	// switch strings.ToLower(s.definition.Image.Variant) {
	// case "default", "rolling":
	// 	url, err := getLatestReleaseURL()
	// 	// rolling release

	// 	// baseURL := fmt.Sprintf("https://github.com/vyos/vyos-rolling-nightly-builds/releases/download/", )

	// case "legacy":
	//	// legacy release

	// case "manual":
	// set url

	var err error
	var fpath string

	baseURL := s.definition.Source.URL
	s.fname = fmt.Sprintf("vyos-1.2.9-S1-amd64.iso")

	url, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("Failed to parse URL %q: %w", baseURL, err)
	}

	checksumFile := ""
	// Force gpg checks when using http
	if !s.definition.Source.SkipVerification && url.Scheme != "https" {
		if len(s.definition.Source.Keys) == 0 {
			return errors.New("GPG keys are required if downloading from HTTP")
		}

		checksumFile = baseURL + "SHA256SUMS"
		fpath, err = s.DownloadHash(s.definition.Image, baseURL+"SHA256SUMS.gpg", "", nil)
		if err != nil {
			return fmt.Errorf("Failed to download %q: %w", baseURL+"SHA256SUMS.gpg", err)
		}

		_, err = s.DownloadHash(s.definition.Image, checksumFile, "", nil)
		if err != nil {
			return fmt.Errorf("Failed to download %q: %w", checksumFile, err)
		}

		valid, err := s.VerifyFile(
			filepath.Join(fpath, "SHA256SUMS"),
			filepath.Join(fpath, "SHA256SUMS.gpg"))
		if err != nil {
			return fmt.Errorf(`Failed to verify "SHA256SUMS": %w`, err)
		}

		if !valid {
			return errors.New(`Invalid signature for "SHA256SUMS"`)
		}
	}

	s.fpath, err = s.DownloadHash(s.definition.Image, baseURL+s.fname, checksumFile, sha256.New())

	return nil
}

// func (s *vyos) getLatestReleaseURL() (string, error) {
// 	apiURL := "https://api.github.com/repos/vyos/vyos-rolling-nightly-builds/releases/latest"

// 	resp, err := http.Get(apiURL)
// 	if err != nil {
// 		return nil, fmt.Errorf("Failed to get latest version information: %q", err)
// 	}
// 	defer resp.Body.Close()

// 	if resp.StatusCode != 200 {
// 		return nil, fmt.Errorf("Failed to get latest version information: %q", resp.StatusCode)
// 	}

// 	body, _ := io.ReadAll(resp.Body)

// 	return latestURL, nil
// }

// func (s *vyos) Run() error {
// 	isoURL := "https://s3-us.vyos.io/rolling/current/vyos-rolling-latest.iso"

// 	fpath, err := s.DownloadHash(s.definition.Image, isoURL, "", nil)
// 	if err != nil {
// 		return fmt.Errorf("Failed downloading ISO: %w", err)
// 	}

// 	err = s.unpackISO(filepath.Join(fpath, "vyos-rolling-latest.iso"), s.rootfsDir)
// 	if err != nil {
// 		return fmt.Errorf("Failed unpacking ISO: %w", err)
// 	}

// 	return nil
// }

func (s *vyos) unpackISO(filePath string, rootfsDir string) error {
	isoDir, err := os.MkdirTemp(s.cacheDir, "temp_")
	if err != nil {
		return fmt.Errorf("Failed creating temporary directory: %w", err)
	}

	defer os.RemoveAll(isoDir)

	squashfsDir, err := os.MkdirTemp(s.cacheDir, "temp_")
	if err != nil {
		return fmt.Errorf("Failed creating temporary directory: %w", err)
	}

	defer os.RemoveAll(squashfsDir)

	// this is easier than doing the whole loop thing ourselves
	err = shared.RunCommand(s.ctx, nil, nil, "mount", "-t", "iso9660", "-o", "ro", filePath, isoDir)
	if err != nil {
		return fmt.Errorf("Failed mounting %q: %w", filePath, err)
	}

	defer func() {
		_ = unix.Unmount(isoDir, 0)
	}()

	squashfsImage := filepath.Join(isoDir, "live", "filesystem.squashfs")

	// The squashfs.img contains an image containing the rootfs, so first
	// mount squashfs.img
	err = shared.RunCommand(s.ctx, nil, nil, "mount", "-t", "squashfs", "-o", "ro", squashfsImage, squashfsDir)
	if err != nil {
		return fmt.Errorf("Failed mounting %q: %w", squashfsImage, err)
	}

	defer func() {
		_ = unix.Unmount(squashfsDir, 0)
	}()

	// Remove rootfsDir otherwise rsync will copy the content into the directory
	// itself
	err = os.RemoveAll(rootfsDir)
	if err != nil {
		return fmt.Errorf("Failed removing directory %q: %w", rootfsDir, err)
	}

	s.logger.WithField("file", squashfsImage).Info("Unpacking root image")

	// Since rootfs is read-only, we need to copy it to a temporary rootfs
	// directory in order to create the minimal rootfs.
	err = shared.RsyncLocal(s.ctx, squashfsDir+"/", rootfsDir)
	if err != nil {
		return fmt.Errorf("Failed running rsync: %w", err)
	}

	return nil
}
