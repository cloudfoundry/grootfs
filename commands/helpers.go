package commands

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"code.cloudfoundry.org/commandrunner/linux_command_runner"
	"code.cloudfoundry.org/grootfs/base_image_puller"
	"code.cloudfoundry.org/grootfs/base_image_puller/unpacker"
	"code.cloudfoundry.org/grootfs/commands/config"
	"code.cloudfoundry.org/grootfs/groot"
	"code.cloudfoundry.org/grootfs/sandbox"
	"code.cloudfoundry.org/grootfs/store/filesystems/namespaced"
	"code.cloudfoundry.org/grootfs/store/image_manager"
	"code.cloudfoundry.org/lager"
	"github.com/opencontainers/runc/libcontainer/user"
)

type fileSystemDriver interface {
	CreateImage(logger lager.Logger, spec image_manager.ImageDriverSpec) (groot.MountInfo, error)
	DestroyImage(logger lager.Logger, path string) error
	FetchStats(logger lager.Logger, path string) (groot.VolumeStats, error)
	ConfigureStore(logger lager.Logger, storePath string, ownerUID, ownerGID int) error
	ValidateFileSystem(logger lager.Logger, path string) error
	InitFilesystem(logger lager.Logger, filesystemPath, storePath string) error
	MountFilesystem(logger lager.Logger, filesystemPath, storePath string) error
	DeInitFilesystem(logger lager.Logger, storePath string) error
	VolumePath(logger lager.Logger, id string) (string, error)
	Volumes(logger lager.Logger) ([]string, error)
	VolumeSize(lager.Logger, string) (int64, error)
	CreateVolume(logger lager.Logger, parentID, id string) (string, error)
	DestroyVolume(logger lager.Logger, id string) error
	MoveVolume(logger lager.Logger, from, to string) error
	WriteVolumeMeta(logger lager.Logger, id string, data base_image_puller.VolumeMeta) error
	HandleOpaqueWhiteouts(logger lager.Logger, id string, opaqueWhiteouts []string) error
	Marshal(logger lager.Logger) ([]byte, error)
	MarkVolumeArtifacts(logger lager.Logger, id string) error
}

func createImageDriver(logger lager.Logger, cfg config.Config, fsDriver fileSystemDriver) (*namespaced.Driver, error) {
	storeNamespacer := groot.NewStoreNamespacer(cfg.StorePath)
	idMappings, err := storeNamespacer.Read()
	if err != nil {
		return nil, err
	}

	shouldCloneUserNs := hasIDMappings(idMappings) && os.Getuid() != 0
	runner := linux_command_runner.New()
	idMapper := unpacker.NewIDMapper(cfg.NewuidmapBin, cfg.NewgidmapBin, runner)
	reexecer := sandbox.NewReexecer(logger, idMapper, idMappings)
	return namespaced.New(fsDriver, reexecer, shouldCloneUserNs), nil
}

func parseIDMappings(args []string) ([]groot.IDMappingSpec, error) {
	mappings := []groot.IDMappingSpec{}

	for _, v := range args {
		var mapping groot.IDMappingSpec
		_, err := fmt.Sscanf(v, "%d:%d:%d", &mapping.NamespaceID, &mapping.HostID, &mapping.Size)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}

	return mappings, nil
}

func readSubUIDMapping(username string) ([]groot.IDMappingSpec, error) {
	user, err := user.LookupUser(username)
	if err != nil {
		return nil, err
	}

	return readSubIDMapping(username, user.Uid, "/etc/subuid")
}

func readSubGIDMapping(groupname string) ([]groot.IDMappingSpec, error) {
	group, err := user.LookupGroup(groupname)
	if err != nil {
		return nil, err
	}

	return readSubIDMapping(groupname, group.Gid, "/etc/subgid")
}

func readSubIDMapping(name string, id int, subidPath string) ([]groot.IDMappingSpec, error) {
	mappings := []groot.IDMappingSpec{{
		HostID: id, NamespaceID: 0, Size: 1,
	}}

	contents, err := ioutil.ReadFile(subidPath)
	if err != nil {
		return nil, err
	}

	for _, line := range strings.Fields(string(contents)) {
		entry := strings.Split(line, ":")
		if entry[0] == name {
			hostID, err := strconv.Atoi(entry[1])
			if err != nil {
				return nil, err
			}
			size, err := strconv.Atoi(entry[2])
			if err != nil {
				return nil, err
			}
			mappings = append(mappings, groot.IDMappingSpec{
				HostID:      hostID,
				NamespaceID: 1,
				Size:        size,
			})
		}
	}

	return mappings, nil
}

func hasIDMappings(idMappings groot.IDMappings) bool {
	return len(idMappings.UIDMappings) > 0 || len(idMappings.GIDMappings) > 0
}
