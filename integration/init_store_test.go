package integration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"code.cloudfoundry.org/grootfs/groot"
	grootfsRunner "code.cloudfoundry.org/grootfs/integration/runner"
	"code.cloudfoundry.org/grootfs/store/filesystems/loopback"
	"code.cloudfoundry.org/grootfs/testhelpers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"golang.org/x/sys/unix"
)

var _ = Describe("Init Store", func() {
	const (
		rootUID = 0
		rootGID = 0
	)

	var (
		runner           grootfsRunner.Runner
		spec             grootfsRunner.InitSpec
		backingStoreFile string
	)

	BeforeEach(func() {
		spec = grootfsRunner.InitSpec{}
		backingStoreFile = fmt.Sprintf("%s.backing-store", StorePath)
		runner = Runner.WithStore(StorePath).SkipInitStore()
	})

	It("returns a newly created store path", func() {
		Expect(runner.InitStore(spec)).To(Succeed())

		Expect(runner.StorePath).To(BeADirectory())

		stat, err := os.Stat(runner.StorePath)
		Expect(err).NotTo(HaveOccurred())
		Expect(stat.Mode().Perm()).To(Equal(os.FileMode(0700)))
	})

	It("sets the ownership to the caller user", func() {
		Expect(runner.InitStore(spec)).To(Succeed())

		Expect(runner.StorePath).To(BeADirectory())

		var stat unix.Stat_t
		Expect(unix.Stat(runner.StorePath, &stat)).To(Succeed())
		Expect(stat.Uid).To(Equal(uint32(rootUID)))
		Expect(stat.Gid).To(Equal(uint32(rootGID)))
	})

	Describe("backing store file configuration", func() {
		Context("when direct-io is not requested", func() {
			BeforeEach(func() {
				spec.WithoutDirectIO = true
			})

			It("does not enable direct-io on the loopback device", func() {
				Expect(runner.InitStore(spec)).To(Succeed())
				loopDev, err := loopback.NewLoSetup().FindAssociatedLoopDevice(runner.StorePath + ".backing-store")
				Expect(err).NotTo(HaveOccurred())
				Expect(testhelpers.IsDirectIOEnabled(loopDev)).To(BeFalse())
			})
		})

		Context("when init.store_size_bytes is passed in config", func() {
			var configFile *os.File

			BeforeEach(func() {
				storeSizeBytes := 600 * 1024 * 1024

				var err error
				configFile, err = os.CreateTemp("", "")
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(configFile.Name(), []byte(fmt.Sprintf(`
init:
  store_size_bytes: %d
`, storeSizeBytes)), 0644)).To(Succeed())
				spec.StoreSizeBytes = -1

				runner = runner.WithConfig(configFile.Name())
			})

			It("creates the backing file with the correct size", func() {
				err := runner.InitStore(spec)
				Expect(err).NotTo(HaveOccurred())

				stat, err := os.Stat(backingStoreFile)
				Expect(err).ToNot(HaveOccurred())
				Expect(stat.Size()).To(Equal(int64(600 * 1024 * 1024)))
			})
		})

		Context("when --store-size-bytes is passed", func() {
			BeforeEach(func() {
				spec.StoreSizeBytes = 600 * 1024 * 1024
			})

			It("creates the backing file with the correct size", func() {
				err := runner.InitStore(spec)
				Expect(err).NotTo(HaveOccurred())

				stat, err := os.Stat(backingStoreFile)
				Expect(err).ToNot(HaveOccurred())
				Expect(stat.Size()).To(Equal(int64(600 * 1024 * 1024)))
			})

			It("initialises a filesystem in the backing file", func() {
				Expect(runner.InitStore(spec)).To(Succeed())

				buffer := gbytes.NewBuffer()
				cmd := exec.Command("file", backingStoreFile)
				cmd.Stdout = buffer
				Expect(cmd.Run()).To(Succeed())

				Expect(buffer).To(gbytes.Say("XFS"))
			})

			Context("when the given store path is already initialized", func() {
				BeforeEach(func() {
					Expect(runner.InitStore(spec)).To(Succeed())
				})

				It("logs the event", func() {
					logs := gbytes.NewBuffer()
					Expect(runner.WithStderr(logs).InitStore(spec)).To(Succeed())

					Expect(logs).To(gbytes.Say("store-already-initialized"))
				})
			})

			Context("but the backing store file is not mounted", func() {
				BeforeEach(func() {
					Expect(runner.InitStore(spec)).To(Succeed())
					Expect(os.WriteFile(filepath.Join(StorePath, "test"), []byte{}, 0777)).To(Succeed())
					Expect(syscall.Unmount(StorePath, 0)).To(Succeed())
					Eventually(queryStoreMountInfo(StorePath)).Should(BeEmpty())
				})

				It("remounts it", func() {
					Expect(runner.InitStore(spec)).To(Succeed())
					Expect(filepath.Join(StorePath, "test")).To(BeAnExistingFile())
				})
			})
		})

		Context("when init.with_direct_io is passed in config", func() {
			var configFile *os.File

			BeforeEach(func() {
				spec.WithoutDirectIO = true
				var err error
				configFile, err = os.CreateTemp("", "")
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(configFile.Name(), []byte(`
init:
  with_direct_io: true`), 0644)).To(Succeed())

				runner = runner.WithConfig(configFile.Name())
			})

			It("enables direct IO on the loopback device", func() {
				err := runner.InitStore(spec)
				Expect(err).NotTo(HaveOccurred())

				loopDev, err := loopback.NewLoSetup().FindAssociatedLoopDevice(runner.StorePath + ".backing-store")
				Expect(err).NotTo(HaveOccurred())
				Expect(testhelpers.IsDirectIOEnabled(loopDev)).To(BeTrue())
			})
		})

		Context("when --with-direct-io is provided", func() {
			BeforeEach(func() {
				spec.WithoutDirectIO = false
			})

			It("enables direct IO on the loopback device", func() {
				err := runner.InitStore(spec)
				Expect(err).NotTo(HaveOccurred())

				loopDev, err := loopback.NewLoSetup().FindAssociatedLoopDevice(runner.StorePath + ".backing-store")
				Expect(err).NotTo(HaveOccurred())
				Expect(testhelpers.IsDirectIOEnabled(loopDev)).To(BeTrue())
			})

		})

		Context("when direct IO is already enabled", func() {
			BeforeEach(func() {
				spec.WithoutDirectIO = false
				Expect(runner.InitStore(spec)).To(Succeed())
			})

			It("does not change the direct IO flag on the loopback device", func() {
				err := runner.InitStore(spec)
				Expect(err).NotTo(HaveOccurred())

				loopDev, err := loopback.NewLoSetup().FindAssociatedLoopDevice(runner.StorePath + ".backing-store")
				Expect(err).NotTo(HaveOccurred())
				Expect(testhelpers.IsDirectIOEnabled(loopDev)).To(BeTrue())
			})

			Context("when the store is reinitialised with direct IO disabled", func() {
				BeforeEach(func() {
					spec.WithoutDirectIO = true
				})

				It("disables direct IO on the loopback device", func() {
					err := runner.InitStore(spec)
					Expect(err).NotTo(HaveOccurred())

					loopDev, err := loopback.NewLoSetup().FindAssociatedLoopDevice(runner.StorePath + ".backing-store")
					Expect(err).NotTo(HaveOccurred())
					Expect(testhelpers.IsDirectIOEnabled(loopDev)).To(BeFalse())
				})
			})
		})

		Context("when the store is already initialized", func() {
			JustBeforeEach(func() {
				Expect(runner.InitStore(spec)).To(Succeed())
			})

			Context("when direct IO is not enabled on the loop device", func() {
				BeforeEach(func() {
					spec.WithoutDirectIO = true
				})

				Context("when direct IO is requested", func() {
					BeforeEach(func() {
						spec.WithoutDirectIO = false
					})

					It("enables direct-io on the loopback device", func() {
						Expect(runner.InitStore(spec)).To(Succeed())
						loopDev, err := loopback.NewLoSetup().FindAssociatedLoopDevice(backingStoreFile)
						Expect(err).NotTo(HaveOccurred())
						Expect(testhelpers.IsDirectIOEnabled(loopDev)).To(BeTrue())
					})
				})
			})

			Context("when the backing file does not exist", func() {
				JustBeforeEach(func() {
					Expect(os.RemoveAll(backingStoreFile)).To(Succeed())
				})

				It("does succesfully reinit the store", func() {
					Expect(runner.InitStore(spec)).To(Succeed())
				})

				It("does not create a backing store file", func() {
					Expect(runner.InitStore(spec)).To(Succeed())
					Expect(backingStoreFile).NotTo(BeAnExistingFile())
				})
			})
		})
	})

	Context("when id mappings are provided", func() {
		BeforeEach(func() {
			spec.UIDMappings = []groot.IDMappingSpec{
				groot.IDMappingSpec{HostID: GrootUID, NamespaceID: 0, Size: 1},
				groot.IDMappingSpec{HostID: 100000, NamespaceID: 1, Size: 65000},
			}
			spec.GIDMappings = []groot.IDMappingSpec{
				groot.IDMappingSpec{HostID: GrootGID, NamespaceID: 0, Size: 1},
				groot.IDMappingSpec{HostID: 100000, NamespaceID: 1, Size: 65000},
			}
		})

		It("sets the ownership to the user mapped as root", func() {
			Expect(runner.InitStore(spec)).To(Succeed())

			Expect(runner.StorePath).To(BeADirectory())

			var stat unix.Stat_t
			Expect(unix.Stat(runner.StorePath, &stat)).To(Succeed())
			Expect(stat.Uid).To(Equal(uint32(GrootUID)))
			Expect(stat.Gid).To(Equal(uint32(GrootGID)))
		})
	})

	Context("when the given store path is already initialized", func() {
		BeforeEach(func() {
			Expect(runner.InitStore(spec)).To(Succeed())
		})

		It("does not return an error", func() {
			Expect(runner.InitStore(spec)).To(Succeed())
		})
	})

	Context("when the user is not root", func() {
		It("returns an error", func() {
			err := runner.RunningAsUser(uint32(GrootUID), uint32(GrootUID)).InitStore(spec)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(fmt.Sprintf("store %s can only be initialized by Root user", runner.StorePath)))
		})
	})
})

func queryStoreMountInfo(path string) func() string {
	return func() string {
		outBytes, _ := exec.Command("/bin/sh", "-c", "/bin/cat /proc/self/mountinfo | grep "+path).Output()
		return string(outBytes)
	}
}
