package store

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"code.cloudfoundry.org/lager/v3"
	errorspkg "github.com/pkg/errors"
)

//go:generate counterfeiter . VolumeDriver
//go:generate counterfeiter . UnusedVolumeGetter

type VolumeDriver interface {
	VolumeSize(lager.Logger, string) (int64, error)
	Volumes(lager.Logger) ([]string, error)
}

type UnusedVolumeGetter interface {
	UnusedVolumes(lager.Logger) ([]string, error)
}

type StoreMeasurer struct {
	storePath          string
	volumeDriver       VolumeDriver
	unusedVolumeGetter UnusedVolumeGetter
}

func NewStoreMeasurer(storePath string, volumeDriver VolumeDriver, unusedVolumeGetter UnusedVolumeGetter) *StoreMeasurer {
	return &StoreMeasurer{
		storePath:          storePath,
		volumeDriver:       volumeDriver,
		unusedVolumeGetter: unusedVolumeGetter,
	}
}

func (s *StoreMeasurer) UnusedVolumesSize(logger lager.Logger) (int64, error) {
	unusedVols, err := s.unusedVolumeGetter.UnusedVolumes(logger)
	if err != nil {
		return 0, err
	}
	return s.countVolumesSize(logger, unusedVols)
}

func (s *StoreMeasurer) UsedVolumesSize(logger lager.Logger) (int64, error) {

	totalVolumesSize, err := s.TotalVolumesSize(logger)
	if err != nil {
		return 0, err
	}
	unusedVolumesSize, err := s.UnusedVolumesSize(logger)
	if err != nil {
		return 0, err
	}
	usedVolumesSize := totalVolumesSize - unusedVolumesSize
	return usedVolumesSize, nil
}

func (s *StoreMeasurer) TotalVolumesSize(logger lager.Logger) (int64, error) {
	vols, err := s.volumeDriver.Volumes(logger)
	if err != nil {
		return 0, err
	}
	return s.countVolumesSize(logger, vols)
}

func (s *StoreMeasurer) countVolumesSize(logger lager.Logger, volumes []string) (int64, error) {
	var size int64
	for _, volume := range volumes {
		volumeSize, err := s.volumeDriver.VolumeSize(logger, volume)
		if err != nil && !os.IsNotExist(err) {
			return 0, err
		}
		size += volumeSize
	}

	return size, nil
}

func (s *StoreMeasurer) CommittedQuota(logger lager.Logger) (int64, error) {
	logger = logger.Session("measuring-committed-size")
	logger.Debug("starting")
	defer logger.Debug("ending")

	var totalCommittedSpace int64

	imageDir := filepath.Join(s.storePath, "images")
	files, err := os.ReadDir(imageDir)
	if err != nil {
		return 0, errorspkg.Wrapf(err, "Cannot list images in %s", imageDir)
	}

	for _, file := range files {
		if !file.IsDir() {
			continue
		}

		imageQuota, err := readImageQuota(filepath.Join(imageDir, file.Name()))
		if err != nil && !os.IsNotExist(err) {
			return 0, err
		}

		totalCommittedSpace += imageQuota
	}

	return totalCommittedSpace, nil
}

func readImageQuota(imageDir string) (int64, error) {
	quotaFilePath := filepath.Join(imageDir, "image_quota")
	imageQuotaBytes, err := os.ReadFile(quotaFilePath)
	if err != nil {
		return 0, err
	}

	if len(imageQuotaBytes) == 0 {
		return 0, nil
	}

	imageQuota, err := strconv.ParseInt(string(imageQuotaBytes), 10, 64)
	if err != nil {
		return 0, err
	}

	return imageQuota, nil
}

func (s *StoreMeasurer) PathStats(path string) (totalBytes, UsedBytes int64, err error) {
	stats := syscall.Statfs_t{}
	if err = syscall.Statfs(s.storePath, &stats); err != nil {
		return 0, 0, errorspkg.Wrapf(err, "Invalid path %s", s.storePath)
	}

	// #nosec - file + filesystem sizes will never be negative. changing here will massively impact the codebase's use of int64 throughout public interfaces
	bsize := uint64(stats.Bsize)
	free := stats.Bfree * bsize
	total := stats.Blocks * bsize
	// #nosec -  this won't overflow until we have filesystems reaching 9.2 exabytes (18_446_744_073_709_551_615 / 2). changing here will massively impact the codebase's use of int64 throughout public interfaces
	used := int64(total - free)

	// #nosec -  this won't overflow until we have filesystems reaching 9.2 exabytes (18_446_744_073_709_551_615 / 2). changing here will massively impact the codebase's use of int64 throughout public interfaces
	return int64(total), used, nil
}
