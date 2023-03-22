package overlayxfs_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"code.cloudfoundry.org/grootfs/base_image_puller"
	"code.cloudfoundry.org/grootfs/store"
	"code.cloudfoundry.org/grootfs/store/filesystems"
	"code.cloudfoundry.org/grootfs/store/filesystems/overlayxfs"
	fakes "code.cloudfoundry.org/grootfs/store/filesystems/overlayxfs/overlayxfsfakes"
	"code.cloudfoundry.org/grootfs/store/image_manager"
	"code.cloudfoundry.org/grootfs/testhelpers"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"github.com/docker/docker/pkg/system"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/st3v/glager"
	"golang.org/x/sys/unix"
)

const (
	_        = iota
	kb int64 = 1 << (10 * iota)
	mb
	gb
)

var _ = Describe("Driver", func() {
	var (
		storePath     string
		driver        *overlayxfs.Driver
		logger        *lagertest.TestLogger
		spec          image_manager.ImageDriverSpec
		randomID      string
		randomImageID string
		tardisBinPath string
		unmounter     *fakes.FakeUnmounter
		directIO      *fakes.FakeDirectIO
	)

	BeforeEach(func() {
		unmounter = new(fakes.FakeUnmounter)
		unmounter.UnmountStub = func(log lager.Logger, path string) error {
			return unix.Unmount(path, 0)
		}
		directIO = new(fakes.FakeDirectIO)

		tardisBinPath = filepath.Join(os.TempDir(), fmt.Sprintf("tardis-%d", rand.Int()))
		testhelpers.CopyFile(TardisBinPath, tardisBinPath)
		testhelpers.SuidBinary(tardisBinPath)

		randomImageID = testhelpers.NewRandomID()
		randomID = randVolumeID()
		logger = lagertest.NewTestLogger("overlay+xfs")
		var err error
		storePath, err = ioutil.TempDir(StorePath, "")
		Expect(err).ToNot(HaveOccurred())
		driver = overlayxfs.NewDriver(storePath, tardisBinPath, unmounter, directIO)

		Expect(os.MkdirAll(storePath, 0777)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(storePath, store.VolumesDirName), 0777)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(storePath, store.MetaDirName), 0777)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(storePath, store.ImageDirName), 0777)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(storePath, overlayxfs.LinksDirName), 0777)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(storePath, overlayxfs.IDDir), 0777)).To(Succeed())

		imagePath := filepath.Join(storePath, store.ImageDirName, randomImageID)
		Expect(os.Mkdir(imagePath, 0755)).To(Succeed())

		spec = image_manager.ImageDriverSpec{
			ImagePath: imagePath,
			Mount:     true,
			OwnerUID:  123,
			OwnerGID:  456,
		}
	})

	AfterEach(func() {
		testhelpers.CleanUpOverlayMounts(storePath)
		Expect(os.RemoveAll(storePath)).To(Succeed())
	})

	Describe("InitFilesystem", func() {
		var fsFile string

		BeforeEach(func() {
			tempFile, err := ioutil.TempFile("", "xfs-filesystem")
			Expect(err).NotTo(HaveOccurred())
			fsFile = tempFile.Name()
			Expect(os.Truncate(fsFile, gb)).To(Succeed())

			storePath, err = ioutil.TempDir("", "store")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			_ = syscall.Unmount(storePath, 0)
		})

		It("succcesfully creates and mounts a filesystem", func() {
			mustSucceed(driver.InitFilesystem(logger, fsFile, storePath))
			statfs := syscall.Statfs_t{}
			Expect(syscall.Statfs(storePath, &statfs)).To(Succeed())
			Expect(statfs.Type).To(Equal(filesystems.XfsType))
		})

		It("successfully mounts the filesystem with the correct mount options", func() {
			mustSucceed(driver.InitFilesystem(logger, fsFile, storePath))
			mountinfo, err := ioutil.ReadFile("/proc/self/mountinfo")
			Expect(err).NotTo(HaveOccurred())

			Expect(string(mountinfo)).To(MatchRegexp(fmt.Sprintf("%s[^\n]*noatime[^\n]*prjquota", storePath)))
		})

		Context("when creating the filesystem fails", func() {
			It("returns an error", func() {
				err := driver.InitFilesystem(logger, "/tmp/no-valid", storePath)
				Expect(err).To(MatchError(ContainSubstring("Formatting XFS filesystem")))
			})
		})

		Context("when the filesystem is already formatted", func() {
			BeforeEach(func() {
				cmd := exec.Command("mkfs.xfs", "-f", fsFile)
				Expect(os.Truncate(fsFile, 200*mb)).To(Succeed())
				Expect(cmd.Run()).To(Succeed())
			})

			It("succeeds", func() {
				mustSucceed(driver.InitFilesystem(logger, fsFile, storePath))
			})
		})

		Context("when the store is already mounted", func() {
			BeforeEach(func() {
				Expect(os.Truncate(fsFile, 200*mb)).To(Succeed())
				cmd := exec.Command("mkfs.xfs", "-f", fsFile)
				Expect(cmd.Run()).To(Succeed())
				cmd = exec.Command("mount", "-o", "loop,pquota,noatime", "-t", "xfs", fsFile, storePath)
				Expect(cmd.Run()).To(Succeed())
			})

			It("succeeds", func() {
				mustSucceed(driver.InitFilesystem(logger, fsFile, storePath))
			})
		})

		Context("when mounting the filesystem fails", func() {
			It("returns an error", func() {
				err := driver.InitFilesystem(logger, fsFile, "/tmp/no-valid")
				Expect(err).To(MatchError(ContainSubstring("Mounting filesystem")))
			})
		})

	})

	Describe("MountFilesystem", func() {
		var fsFile string

		BeforeEach(func() {
			tempFile, err := ioutil.TempFile("", "xfs-filesystem")
			Expect(err).NotTo(HaveOccurred())
			fsFile = tempFile.Name()
			Expect(os.Truncate(fsFile, gb)).To(Succeed())
			cmd := exec.Command("mkfs.xfs", "-f", fsFile)
			Expect(cmd.Run()).To(Succeed())

			storePath, err = ioutil.TempDir("", "store")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			_ = syscall.Unmount(storePath, 0)
		})

		It("succcesfully mounts the filesystem", func() {
			Expect(driver.MountFilesystem(logger, fsFile, storePath)).To(Succeed())
			statfs := syscall.Statfs_t{}
			Expect(syscall.Statfs(storePath, &statfs)).To(Succeed())
			Expect(statfs.Type).To(Equal(filesystems.XfsType))
		})

		It("successfully mounts the filesystem with the correct mount options", func() {
			Expect(driver.MountFilesystem(logger, fsFile, storePath)).To(Succeed())
			mountinfo, err := ioutil.ReadFile("/proc/self/mountinfo")
			Expect(err).NotTo(HaveOccurred())

			Expect(string(mountinfo)).To(MatchRegexp(fmt.Sprintf("%s[^\n]*noatime[^\n]*prjquota", storePath)))
		})

		Context("when the store is already mounted", func() {
			BeforeEach(func() {
				Expect(os.Truncate(fsFile, 200*mb)).To(Succeed())
				cmd := exec.Command("mkfs.xfs", "-f", fsFile)
				Expect(cmd.Run()).To(Succeed())
				cmd = exec.Command("mount", "-o", "loop,pquota,noatime", "-t", "xfs", fsFile, storePath)
				Expect(cmd.Run()).To(Succeed())
			})

			It("returns an error", func() {
				Expect(driver.MountFilesystem(logger, fsFile, storePath)).To(MatchError(ContainSubstring("already mounted")))
			})
		})

		Context("when mounting the filesystem fails", func() {
			It("returns an error", func() {
				err := driver.MountFilesystem(logger, fsFile, "/tmp/no-valid")
				Expect(err).To(MatchError(ContainSubstring("Mounting filesystem")))
			})
		})
	})

	Describe("DeInitFilesystem", func() {
		var fsFile, deinitStorePath string

		BeforeEach(func() {
			tempFile, err := ioutil.TempFile("", "xfs-filesystem")
			Expect(err).NotTo(HaveOccurred())
			fsFile = tempFile.Name()
			Expect(os.Truncate(fsFile, gb)).To(Succeed())

			deinitStorePath, err = ioutil.TempDir("", "store")
			Expect(err).NotTo(HaveOccurred())
			mustSucceed(driver.InitFilesystem(logger, fsFile, deinitStorePath))
			Expect(testhelpers.XFSMountPoints()).To(ContainElement(deinitStorePath))
		})

		It("succcesfully unmounts a filesystem", func() {
			Expect(driver.DeInitFilesystem(logger, deinitStorePath)).To(Succeed())
			Expect(unmounter.UnmountCallCount()).To(Equal(1))
			_, path := unmounter.UnmountArgsForCall(0)
			Expect(path).To(Equal(deinitStorePath))
		})

		Context("when the unmount returns an error", func() {
			BeforeEach(func() {
				unmounter.UnmountReturns(errors.New("unmount-failed"))
			})

			It("returns the error", func() {
				Expect(driver.DeInitFilesystem(logger, "some/path")).To(MatchError(ContainSubstring("unmount-failed")))
			})
		})
	})

	Describe("CreateImage", func() {
		var (
			layer1ID   string
			layer2ID   string
			layer1Path string
			layer2Path string
		)

		BeforeEach(func() {
			layer1ID = randVolumeID()
			layer1Path = createVolume(storePath, driver, "parent-id", layer1ID, 5000)
			Expect(ioutil.WriteFile(filepath.Join(layer1Path, "file-hello"), []byte("hello-1"), 0755)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(layer1Path, "file-bye"), []byte("bye-1"), 0700)).To(Succeed())
			Expect(os.Mkdir(filepath.Join(layer1Path, "a-folder"), 0700)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(layer1Path, "a-folder", "folder-file"), []byte("in-a-folder-1"), 0755)).To(Succeed())

			layer2ID = randVolumeID()
			layer2Path = createVolume(storePath, driver, "parent-id", layer2ID, 10000)
			Expect(ioutil.WriteFile(filepath.Join(layer2Path, "file-bye"), []byte("bye-2"), 0700)).To(Succeed())
			Expect(os.Mkdir(filepath.Join(layer2Path, "a-folder"), 0700)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(layer2Path, "a-folder", "folder-file"), []byte("in-a-folder-2"), 0755)).To(Succeed())

			spec.BaseVolumeIDs = []string{layer1ID}
		})

		It("initializes the image path", func() {
			Expect(filepath.Join(spec.ImagePath, overlayxfs.UpperDir)).ToNot(BeAnExistingFile())
			Expect(filepath.Join(spec.ImagePath, overlayxfs.WorkDir)).ToNot(BeAnExistingFile())
			Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).ToNot(BeAnExistingFile())

			_, err := driver.CreateImage(logger, spec)
			Expect(err).ToNot(HaveOccurred())

			Expect(filepath.Join(spec.ImagePath, overlayxfs.UpperDir)).To(BeADirectory())
			Expect(filepath.Join(spec.ImagePath, overlayxfs.WorkDir)).To(BeADirectory())
			Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).To(BeADirectory())
		})

		It("creates a rootfs with the same files as the volume", func() {
			Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).ToNot(BeAnExistingFile())

			_, err := driver.CreateImage(logger, spec)
			Expect(err).ToNot(HaveOccurred())

			Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).To(BeADirectory())

			contents, err := ioutil.ReadFile(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir, "file-hello"))
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(BeEquivalentTo("hello-1"))

			contents, err = ioutil.ReadFile(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir, "file-bye"))
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(BeEquivalentTo("bye-1"))

			Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir, "a-folder")).To(BeADirectory())

			contents, err = ioutil.ReadFile(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir, "a-folder", "folder-file"))
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(BeEquivalentTo("in-a-folder-1"))
		})

		It("returns a mountJson object", func() {
			mountJson, err := driver.CreateImage(logger, spec)
			Expect(err).ToNot(HaveOccurred())

			Expect(mountJson.Type).To(Equal("overlay"))
			Expect(mountJson.Source).To(Equal("overlay"))
			Expect(mountJson.Destination).To(Equal("/"))
			Expect(mountJson.Options).To(HaveLen(1))
			Expect(mountJson.Options[0]).To(MatchRegexp(fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
				filepath.Join(storePath, overlayxfs.LinksDirName, ".*"),
				filepath.Join(spec.ImagePath, overlayxfs.UpperDir),
				filepath.Join(spec.ImagePath, overlayxfs.WorkDir),
			)))
		})

		Context("when a volume metadata file is missing", func() {
			BeforeEach(func() {
				metaFilePath := filepath.Join(storePath, store.MetaDirName, "volume-"+layer1ID)
				Expect(os.Remove(metaFilePath)).To(Succeed())
			})

			It("logs the occurence and errors", func() {
				_, err := driver.CreateImage(logger, spec)
				Expect(err).To(MatchError(ContainSubstring("calculating base volume size for volume")))
				Eventually(logger).Should(gbytes.Say("calculating-base-volume-size-failed"))
			})
		})

		Context("multi-layer image", func() {
			BeforeEach(func() {
				spec.BaseVolumeIDs = []string{layer1ID, layer2ID}
			})

			It("creates a rootfs with files correctly composed from the layer volumes", func() {
				Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).ToNot(BeAnExistingFile())

				_, err := driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())

				Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).To(BeADirectory())

				contents, err := ioutil.ReadFile(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir, "file-hello"))
				Expect(err).NotTo(HaveOccurred())
				Expect(contents).To(BeEquivalentTo("hello-1"))

				contents, err = ioutil.ReadFile(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir, "file-bye"))
				Expect(err).NotTo(HaveOccurred())
				Expect(contents).To(BeEquivalentTo("bye-2"))

				Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir, "a-folder")).To(BeADirectory())

				contents, err = ioutil.ReadFile(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir, "a-folder", "folder-file"))
				Expect(err).NotTo(HaveOccurred())
				Expect(contents).To(BeEquivalentTo("in-a-folder-2"))
			})
		})

		It("uses the correct permissions and ownerships for the internal folders", func() {
			_, err := driver.CreateImage(logger, spec)
			Expect(err).ToNot(HaveOccurred())

			stat, err := os.Stat(filepath.Join(spec.ImagePath, overlayxfs.UpperDir))
			Expect(err).NotTo(HaveOccurred())
			Expect(stat.Mode().Perm()).To(Equal(os.FileMode(0755)))
			uid, gid := getUidAndGid(stat)
			Expect(uid).To(Equal(123))
			Expect(gid).To(Equal(456))

			stat, err = os.Stat(filepath.Join(spec.ImagePath, overlayxfs.WorkDir))
			Expect(err).NotTo(HaveOccurred())
			Expect(stat.Mode().Perm()).To(Equal(os.FileMode(0755)))
			uid, gid = getUidAndGid(stat)
			Expect(uid).To(Equal(123))
			Expect(gid).To(Equal(456))

			stat, err = os.Stat(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir))
			Expect(err).NotTo(HaveOccurred())
			Expect(stat.Mode().Perm()).To(Equal(os.FileMode(0755)))
			uid, gid = getUidAndGid(stat)
			Expect(uid).To(Equal(123))
			Expect(gid).To(Equal(456))
		})

		Context("when Mount is false", func() {
			BeforeEach(func() {
				spec.Mount = false
			})

			It("does not mount the rootfs", func() {
				_, err := driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())
				rootfsPath := filepath.Join(spec.ImagePath, "rootfs")
				contents, err := ioutil.ReadDir(rootfsPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(contents).To(BeEmpty())
			})

			It("does still create all the subdirectoies", func() {
				Expect(filepath.Join(spec.ImagePath, overlayxfs.UpperDir)).ToNot(BeAnExistingFile())
				Expect(filepath.Join(spec.ImagePath, overlayxfs.WorkDir)).ToNot(BeAnExistingFile())
				Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).ToNot(BeAnExistingFile())

				_, err := driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())

				Expect(filepath.Join(spec.ImagePath, overlayxfs.UpperDir)).To(BeADirectory())
				Expect(filepath.Join(spec.ImagePath, overlayxfs.WorkDir)).To(BeADirectory())
				Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).To(BeADirectory())
			})
		})

		Context("image_info", func() {
			BeforeEach(func() {
				volumeID := randVolumeID()
				createVolume(storePath, driver, "parent-id", volumeID, 5000)

				spec.BaseVolumeIDs = []string{volumeID}
			})

			It("creates a image info file with the total base volume size", func() {
				Expect(filepath.Join(spec.ImagePath, "image_info")).ToNot(BeAnExistingFile())
				_, err := driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())

				ensureQuotaMatches(filepath.Join(spec.ImagePath, "image_info"), 5000)
			})
		})

		Context("when disk limit is 0", func() {
			BeforeEach(func() {
				spec.DiskLimit = 0
			})

			It("doesn't apply any quota", func() {
				_, err := driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())

				Expect(logger).To(glager.ContainSequence(
					glager.Debug(
						glager.Message("overlay+xfs.overlayxfs-creating-image.applying-quotas.no-need-for-quotas"),
					),
				))
			})

			It("does not create an image quota file containing the requested quota", func() {
				_, err := driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())

				Expect(filepath.Join(spec.ImagePath, "image_quota")).ToNot(BeAnExistingFile())
			})
		})

		Context("when disk limit is > 0", func() {
			BeforeEach(func() {
				spec.DiskLimit = 10 * mb
				Expect(ioutil.WriteFile(filepath.Join(storePath, store.MetaDirName, fmt.Sprintf("volume-%s", layer1ID)), []byte(`{"Size": 3145728}`), 0644)).To(Succeed())
			})

			It("creates the storeDevice block device in the `images` parent folder", func() {
				storeDevicePath := filepath.Join(storePath, "storeDevice")

				Expect(storeDevicePath).ToNot(BeAnExistingFile())
				_, err := driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())
				Expect(storeDevicePath).To(BeAnExistingFile())
			})

			It("can overwrite files from the lowerdirs", func() {
				_, err := driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())
				imageRootfsPath := filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)

				dd := exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/file", imageRootfsPath), "count=5", "bs=1M")
				sess, err := gexec.Start(dd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sess).Should(gexec.Exit(0))
			})

			It("allows images to have independent quotas", func() {
				_, err := driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())
				imageRootfsPath := filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)

				dd := exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/file-1", imageRootfsPath), "count=6", "bs=1M")
				sess, err := gexec.Start(dd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sess).Should(gexec.Exit(0))

				anotherSpec := spec
				anotherImagePath, err := ioutil.TempDir(filepath.Join(storePath, store.ImageDirName), "another-image")
				Expect(err).NotTo(HaveOccurred())
				anotherSpec.ImagePath = anotherImagePath
				_, err = driver.CreateImage(logger, anotherSpec)
				Expect(err).ToNot(HaveOccurred())
				anotherImageRootfsPath := filepath.Join(anotherImagePath, overlayxfs.RootfsDir)

				dd = exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/file-2", anotherImageRootfsPath), "count=6", "bs=1M")
				sess, err = gexec.Start(dd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sess).Should(gexec.Exit(0))
			})

			Context("inclusive quota", func() {
				BeforeEach(func() {
					spec.ExclusiveDiskLimit = false
				})

				It("enforces the quota in the image", func() {
					_, err := driver.CreateImage(logger, spec)
					Expect(err).ToNot(HaveOccurred())
					imageRootfsPath := filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)

					dd := exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/file-1", imageRootfsPath), "count=5", "bs=1M")
					sess, err := gexec.Start(dd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())
					Eventually(sess).Should(gexec.Exit(0))

					dd = exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/file-2", imageRootfsPath), "count=4", "bs=1M")
					sess, err = gexec.Start(dd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())
					Eventually(sess, 5*time.Second).Should(gexec.Exit(1))
					Eventually(sess.Err).Should(gbytes.Say("No space left on device"))
				})

				It("creates a image quota file containing the requested quota", func() {
					Expect(filepath.Join(spec.ImagePath, "image_quota")).ToNot(BeAnExistingFile())
					_, err := driver.CreateImage(logger, spec)
					Expect(err).ToNot(HaveOccurred())

					ensureQuotaMatches(filepath.Join(spec.ImagePath, "image_quota"), 10*mb-3145728)
				})

				Context("when the DiskLimit is smaller than VolumeSize", func() {
					It("returns an error", func() {
						spec.DiskLimit = 4000
						_, err := driver.CreateImage(logger, spec)
						Expect(err).To(MatchError(ContainSubstring("disk limit is smaller than volume size")))
					})
				})

				Context("when the DiskLimit is less than the minimum quota (1024*256 bytes) after accounting for the VolumeSize", func() {
					BeforeEach(func() {
						volumeSize := int64(128 * kb)
						layerID := randVolumeID()
						_ = createVolume(storePath, driver, "parent-id", layerID, volumeSize)

						spec.BaseVolumeIDs = []string{layerID}
						spec.DiskLimit = volumeSize + (128 * kb)
					})

					It("enforces the minimum required quota in the image", func() {
						_, err := driver.CreateImage(logger, spec)
						Expect(err).ToNot(HaveOccurred())
						imageRootfsPath := filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)

						dd := exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/file-1", imageRootfsPath), "count=1", "bs=128K")
						sess, err := gexec.Start(dd, GinkgoWriter, GinkgoWriter)
						Expect(err).NotTo(HaveOccurred())
						Eventually(sess).Should(gexec.Exit(0))

						dd = exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/file-2", imageRootfsPath), "count=1", "bs=128K")
						sess, err = gexec.Start(dd, GinkgoWriter, GinkgoWriter)
						Expect(err).NotTo(HaveOccurred())
						Eventually(sess, 5*time.Second).Should(gexec.Exit(1))
						Eventually(sess.Err).Should(gbytes.Say("No space left"))
					})
				})
			})

			Context("exclusive quota", func() {
				BeforeEach(func() {
					spec.ExclusiveDiskLimit = true
				})

				Context("when the DiskLimit is smaller than VolumeSize", func() {
					It("succeeds", func() {
						spec.DiskLimit = 3 * mb
						_, err := driver.CreateImage(logger, spec)
						Expect(err).ToNot(HaveOccurred())
					})
				})

				It("enforces the quota in the image", func() {
					_, err := driver.CreateImage(logger, spec)
					Expect(err).ToNot(HaveOccurred())
					imageRootfsPath := filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)

					dd := exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/file-1", imageRootfsPath), "count=8", "bs=1M")
					sess, err := gexec.Start(dd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())
					Eventually(sess).Should(gexec.Exit(0))

					dd = exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/file-2", imageRootfsPath), "count=8", "bs=1M")
					sess, err = gexec.Start(dd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())
					Eventually(sess, 5*time.Second).Should(gexec.Exit(1))
					Eventually(sess.Err).Should(gbytes.Say("No space left on device"))
				})

				It("creates a image quota file containing the requested quota", func() {
					Expect(filepath.Join(spec.ImagePath, "image_quota")).ToNot(BeAnExistingFile())
					_, err := driver.CreateImage(logger, spec)
					Expect(err).ToNot(HaveOccurred())

					ensureQuotaMatches(filepath.Join(spec.ImagePath, "image_quota"), 10*mb)
				})
			})

			Context("when tardis is not in the path", func() {
				BeforeEach(func() {
					driver = overlayxfs.NewDriver(storePath, "/bin/bananas", unmounter, directIO)
				})

				It("returns an error", func() {
					_, err := driver.CreateImage(logger, spec)
					Expect(err).To(MatchError(ContainSubstring("tardis was not found in the $PATH")))
				})

				It("does not write quota info", func() {
					_, _ = driver.CreateImage(logger, spec)
					Expect(filepath.Join(spec.ImagePath, "image_quota")).ToNot(BeAnExistingFile())
				})
			})
		})

		Context("when base volume folder does not exist", func() {
			BeforeEach(func() {
				testhelpers.UnsuidBinary(tardisBinPath)
			})

			It("returns an error", func() {
				spec.BaseVolumeIDs = []string{"not-real"}
				_, err := driver.CreateImage(logger, spec)
				Expect(err).To(MatchError(ContainSubstring("base volume path does not exist")))
			})
		})

		Context("when image path folder doesn't exist", func() {
			It("returns an error", func() {
				spec.ImagePath = "/not-real"
				_, err := driver.CreateImage(logger, spec)
				Expect(err).To(MatchError(ContainSubstring("image path does not exist")))
			})
		})

		Context("when creating the upper folder fails", func() {
			It("returns an error", func() {
				Expect(os.MkdirAll(filepath.Join(spec.ImagePath, overlayxfs.UpperDir), 0755)).To(Succeed())
				_, err := driver.CreateImage(logger, spec)
				Expect(err).To(MatchError(ContainSubstring("creating upperdir folder")))
			})
		})

		Context("when creating the workdir folder fails", func() {
			It("returns an error", func() {
				Expect(os.MkdirAll(filepath.Join(spec.ImagePath, overlayxfs.WorkDir), 0755)).To(Succeed())
				_, err := driver.CreateImage(logger, spec)
				Expect(err).To(MatchError(ContainSubstring("creating workdir folder")))
			})
		})

		Context("when creating the rootfs folder fails", func() {
			It("returns an error", func() {
				Expect(os.MkdirAll(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir), 0755)).To(Succeed())
				_, err := driver.CreateImage(logger, spec)
				Expect(err).To(MatchError(ContainSubstring("creating rootfs folder")))
			})
		})
	})

	Describe("DestroyImage", func() {
		JustBeforeEach(func() {
			volumeID := randVolumeID()
			createVolume(storePath, driver, "parent-id", volumeID, 3145728)

			spec.BaseVolumeIDs = []string{volumeID}
			_, err := driver.CreateImage(logger, spec)
			Expect(err).ToNot(HaveOccurred())
		})

		It("unmounts the rootfs dir", func() {
			Expect(driver.DestroyImage(logger, spec.ImagePath)).To(Succeed())
			Expect(unmounter.UnmountCallCount()).To(Equal(1))
			_, unmountPath := unmounter.UnmountArgsForCall(0)
			Expect(unmountPath).To(Equal(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)))
		})

		It("removes upper, work and rootfs dir from the image path", func() {
			Expect(filepath.Join(spec.ImagePath, overlayxfs.UpperDir)).To(BeADirectory())
			Expect(filepath.Join(spec.ImagePath, overlayxfs.WorkDir)).To(BeADirectory())
			Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).To(BeADirectory())

			Expect(driver.DestroyImage(logger, spec.ImagePath)).To(Succeed())

			Expect(filepath.Join(spec.ImagePath, overlayxfs.UpperDir)).ToNot(BeAnExistingFile())
			Expect(filepath.Join(spec.ImagePath, overlayxfs.WorkDir)).ToNot(BeAnExistingFile())
			Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).ToNot(BeAnExistingFile())
		})

		Context("projectids", func() {
			BeforeEach(func() {
				spec.DiskLimit = 1000000000
			})

			It("removes the projectid directory for that image", func() {
				projectidsDir := filepath.Join(storePath, overlayxfs.IDDir)
				ids, err := ioutil.ReadDir(projectidsDir)
				Expect(err).NotTo(HaveOccurred())
				Expect(ids).To(HaveLen(1))

				Expect(driver.DestroyImage(logger, spec.ImagePath)).To(Succeed())

				ids, err = ioutil.ReadDir(projectidsDir)
				Expect(err).NotTo(HaveOccurred())
				Expect(ids).To(BeEmpty())
			})
		})

		Context("when it fails to unmount the rootfs", func() {
			JustBeforeEach(func() {
				unmounter.UnmountReturns(errors.New("unmount-failed"))
			})

			It("returns an error", func() {
				err := driver.DestroyImage(logger, spec.ImagePath)
				Expect(err).To(MatchError(ContainSubstring("deleting image path")))
			})

			It("does not delete the image directory", func() {
				err := driver.DestroyImage(logger, spec.ImagePath)
				Expect(err).To(HaveOccurred())

				Expect(filepath.Join(spec.ImagePath, overlayxfs.UpperDir)).To(BeADirectory())

				rootfsPath := filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)
				rootfsContent, err := ioutil.ReadDir(rootfsPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(rootfsContent).NotTo(BeEmpty())

			})
		})

		Context("when there is a very long file path in the rootfs", func() {
			It("successfully removes the rootfs", func() {
				Expect(createVeryLongFilePath(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir))).To(Succeed())
				Expect(driver.DestroyImage(logger, spec.ImagePath)).To(Succeed())
				Expect(filepath.Join(spec.ImagePath, overlayxfs.RootfsDir)).NotTo(BeADirectory())
			})
		})
	})

	Describe("FetchStats", func() {
		BeforeEach(func() {
			volumeID := randVolumeID()
			createVolume(storePath, driver, "parent-id", volumeID, 3000000)

			spec.BaseVolumeIDs = []string{volumeID}
			spec.DiskLimit = 10 * mb
			_, err := driver.CreateImage(logger, spec)
			Expect(err).ToNot(HaveOccurred())

			dd := exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s/rootfs/file-1", spec.ImagePath), "count=4", "bs=1M")
			sess, err := gexec.Start(dd, GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())
			Eventually(sess).Should(gexec.Exit(0))
		})

		It("reports the image usage correctly", func() {
			stats, err := driver.FetchStats(logger, spec.ImagePath)
			Expect(err).NotTo(HaveOccurred())

			Expect(stats.DiskUsage.ExclusiveBytesUsed).To(Equal(int64(4202496)))
			Expect(stats.DiskUsage.TotalBytesUsed).To(Equal(int64(3000000 + 4202496)))
		})

		Context("when path does not exist", func() {
			var imagePath string

			BeforeEach(func() {
				imagePath = "/tmp/not-here"
			})

			It("returns an error", func() {
				_, err := driver.FetchStats(logger, imagePath)
				Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("image path (%s) doesn't exist", imagePath))))
			})
		})

		Context("when the path doesn't have a quota", func() {
			BeforeEach(func() {
				tmpDir, err := ioutil.TempDir(filepath.Join(storePath, store.ImageDirName), "")
				Expect(err).NotTo(HaveOccurred())
				spec.DiskLimit = 0
				spec.ImagePath = tmpDir
				_, err = driver.CreateImage(logger, spec)
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns the exclusive bytes used as 0", func() {
				volumeStats, err := driver.FetchStats(logger, spec.ImagePath)
				Expect(err).ToNot(HaveOccurred())
				Expect(volumeStats.DiskUsage.ExclusiveBytesUsed).To(Equal(int64(0)))
				Expect(volumeStats.DiskUsage.TotalBytesUsed).To(BeNumerically("~", 3000000, 100))
			})
		})

		Context("when the path doesn't have an `image_info` file", func() {
			BeforeEach(func() {
				Expect(os.Remove(filepath.Join(spec.ImagePath, "image_info"))).To(Succeed())
			})

			It("returns an error", func() {
				_, err := driver.FetchStats(logger, spec.ImagePath)
				Expect(err).To(MatchError(ContainSubstring("reading image info")))
			})
		})

		Context("when it fails to fetch XFS project ID", func() {
			It("returns an error", func() {
				_, err := driver.FetchStats(logger, "/dev")
				Expect(err).To(MatchError(ContainSubstring("inappropriate ioctl for device")))
			})
		})
	})

	Describe("VolumePath", func() {
		BeforeEach(func() {
			Expect(os.MkdirAll(filepath.Join(storePath, store.VolumesDirName, randomID), 0755)).To(Succeed())
		})

		It("returns the volume path when it exists", func() {
			retVolPath, err := driver.VolumePath(logger, randomID)
			Expect(err).NotTo(HaveOccurred())
			Expect(retVolPath).To(Equal(filepath.Join(storePath, store.VolumesDirName, randomID)))
		})

		Context("when the volume does not exist", func() {
			It("returns an error", func() {
				_, err := driver.VolumePath(logger, "non-existent-id")
				Expect(err).To(MatchError(ContainSubstring("volume does not exist")))
			})
		})
	})

	Describe("ConfigureStore", func() {
		const (
			currentUID = 2001
			currentGID = 2002
		)

		var (
			backingStorePath string
		)

		BeforeEach(func() {
			var err error
			storePath, err = ioutil.TempDir(storePath, "configure-store-test")
			Expect(err).ToNot(HaveOccurred())

			backingStoreFile, err := ioutil.TempFile(storePath, "backing-store-test")
			Expect(err).ToNot(HaveOccurred())

			backingStorePath = backingStoreFile.Name()
		})

		It("creates a links directory", func() {
			Expect(driver.ConfigureStore(logger, storePath, backingStorePath, currentUID, currentGID)).To(Succeed())
			stat, err := os.Stat(filepath.Join(storePath, overlayxfs.LinksDirName))
			Expect(err).NotTo(HaveOccurred())
			Expect(stat.IsDir()).To(BeTrue())
			Expect(stat.Sys().(*syscall.Stat_t).Uid).To(Equal(uint32(currentUID)))
			Expect(stat.Sys().(*syscall.Stat_t).Gid).To(Equal(uint32(currentGID)))
		})

		It("creates a whiteout device", func() {
			Expect(driver.ConfigureStore(logger, storePath, backingStorePath, currentUID, currentGID)).To(Succeed())

			stat, err := os.Stat(filepath.Join(storePath, overlayxfs.WhiteoutDevice))
			Expect(err).NotTo(HaveOccurred())
			Expect(stat.Sys().(*syscall.Stat_t).Uid).To(Equal(uint32(currentUID)))
			Expect(stat.Sys().(*syscall.Stat_t).Gid).To(Equal(uint32(currentGID)))
		})

		It("enables direct IO on the loopback device", func() {
			Expect(driver.ConfigureStore(logger, storePath, backingStorePath, currentUID, currentGID)).To(Succeed())
			Expect(directIO.ConfigureCallCount()).To(Equal(1))
			actualDirectIOPath := directIO.ConfigureArgsForCall(0)
			Expect(actualDirectIOPath).To(Equal(backingStorePath))
		})

		Context("when the backing store path does not exist", func() {
			BeforeEach(func() {
				Expect(os.Remove(backingStorePath)).To(Succeed())
			})

			It("does not attempt to configure direct io", func() {
				Expect(driver.ConfigureStore(logger, storePath, backingStorePath, currentUID, currentGID)).To(Succeed())
				Expect(directIO.ConfigureCallCount()).To(Equal(0))
			})
		})

		Context("when the whiteout 'device' is not a device", func() {
			BeforeEach(func() {
				Expect(os.MkdirAll(storePath, 0755)).To(Succeed())
				Expect(ioutil.WriteFile(filepath.Join(storePath, overlayxfs.WhiteoutDevice), []byte{}, 0755)).To(Succeed())
			})

			It("returns an error", func() {
				err := driver.ConfigureStore(logger, storePath, backingStorePath, currentUID, currentGID)
				Expect(err).To(MatchError(ContainSubstring("the whiteout device file is not a valid device")))
			})
		})

		Context("when enabling direct IO fails", func() {
			BeforeEach(func() {
				directIO.ConfigureReturns(errors.New("lo-error"))
			})

			It("returns the error", func() {
				err := driver.ConfigureStore(logger, storePath, backingStorePath, currentUID, currentGID)
				Expect(err).To(MatchError(ContainSubstring("lo-error")))
			})
		})
	})

	Describe("ValidateFileSystem", func() {
		Context("when storepath is a XFS mount", func() {
			It("returns no error", func() {
				Expect(driver.ValidateFileSystem(logger, storePath)).To(Succeed())
			})
		})

		Context("when storepath is not a XFS mount", func() {
			It("returns an error", func() {
				err := driver.ValidateFileSystem(logger, "/mnt/ext4")
				Expect(err).To(MatchError(ContainSubstring("Store path filesystem (/mnt/ext4) is incompatible with native driver (must be XFS mountpoint)")))
			})
		})
	})

	Describe("CreateVolume", func() {
		It("creates a volume", func() {
			expectedVolumePath := filepath.Join(storePath, store.VolumesDirName, randomID)
			Expect(expectedVolumePath).NotTo(BeAnExistingFile())

			volumePath, err := driver.CreateVolume(logger, "parent-id", randomID)
			Expect(err).NotTo(HaveOccurred())

			Expect(expectedVolumePath).To(BeADirectory())
			Expect(volumePath).To(Equal(expectedVolumePath))

			linkFile := filepath.Join(storePath, overlayxfs.LinksDirName, randomID)
			_, err = os.Stat(linkFile)
			Expect(err).ToNot(HaveOccurred(), "volume link file has not been created")

			linkName, err := ioutil.ReadFile(linkFile)
			Expect(err).ToNot(HaveOccurred(), "failed to read volume link file")

			link := filepath.Join(storePath, overlayxfs.LinksDirName, string(linkName))
			linkStat, err := os.Lstat(link)
			Expect(err).ToNot(HaveOccurred())
			Expect(linkStat.Mode()&os.ModeSymlink).ToNot(
				BeZero(),
				fmt.Sprintf("Volume link %s is not a symlink", link),
			)
			Expect(os.Readlink(link)).To(Equal(volumePath), "Volume link does not point to volume")
		})

		Context("when volume dir doesn't exist", func() {
			BeforeEach(func() {
				Expect(os.RemoveAll(filepath.Join(storePath, store.VolumesDirName))).To(Succeed())
			})

			It("returns an error", func() {
				_, err := driver.CreateVolume(logger, "parent-id", randomID)
				Expect(err).To(MatchError(ContainSubstring("creating volume")))
			})
		})

		Context("when volume already exists", func() {
			BeforeEach(func() {
				Expect(os.Mkdir(filepath.Join(storePath, store.VolumesDirName, randomID), 0755)).To(Succeed())
			})

			It("returns an error", func() {
				_, err := driver.CreateVolume(logger, "parent-id", randomID)
				Expect(err).To(MatchError(ContainSubstring("creating volume")))
			})
		})
	})

	Describe("Volumes", func() {
		var volumesPath string
		BeforeEach(func() {
			volumesPath = filepath.Join(storePath, store.VolumesDirName)
			Expect(os.Mkdir(filepath.Join(volumesPath, "sha256:vol-a"), 0777)).To(Succeed())
			Expect(os.Mkdir(filepath.Join(volumesPath, "sha256:vol-b"), 0777)).To(Succeed())
		})

		AfterEach(func() {
			Expect(os.RemoveAll(volumesPath)).To(Succeed())
		})

		It("returns a list with existing volumes id", func() {
			volumes, err := driver.Volumes(logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(volumes)).To(Equal(2), "incorrect number of volumes")
			Expect(volumes).To(ContainElement("sha256:vol-a"))
			Expect(volumes).To(ContainElement("sha256:vol-b"))
		})

		Context("when fails to list volumes", func() {
			It("returns an error", func() {
				Expect(os.RemoveAll(filepath.Join(storePath, store.VolumesDirName))).To(Succeed())
				_, err := driver.Volumes(logger)
				Expect(err).To(MatchError(ContainSubstring("failed to list volumes")))
			})
		})
	})

	Describe("MarkVolumeArtifacts", func() {
		var (
			metaDirPath string
			linkDirPath string
			volumePath  string
			volumeID    string
		)

		BeforeEach(func() {
			metaDirPath = filepath.Join(storePath, store.MetaDirName)
			linkDirPath = filepath.Join(storePath, overlayxfs.LinksDirName)

			volumeID = randVolumeID()
			var err error
			volumePath, err = driver.CreateVolume(logger, "", volumeID)
			Expect(err).NotTo(HaveOccurred())

			Expect(driver.WriteVolumeMeta(logger, volumeID, base_image_puller.VolumeMeta{Size: kb})).To(Succeed())
		})

		It("Renames the volume directory", func() {
			Expect(driver.MarkVolumeArtifacts(logger, volumeID)).To(Succeed())
			Expect(filepath.Join(filepath.Dir(volumePath), fmt.Sprintf("gc.%s", volumeID))).To(BeADirectory())
		})

		It("Renames the volume's metadata", func() {
			Expect(driver.MarkVolumeArtifacts(logger, volumeID)).To(Succeed())
			Expect(filepath.Join(metaDirPath, fmt.Sprintf("volume-gc.%s", volumeID))).To(BeAnExistingFile())
		})

		It("Renames the volume's link dirs", func() {
			Expect(driver.MarkVolumeArtifacts(logger, volumeID)).To(Succeed())

			newVolumePath := filepath.Join(filepath.Dir(volumePath), fmt.Sprintf("gc.%s", volumeID))
			Expect(filepath.Join(linkDirPath, filepath.Base(newVolumePath))).To(BeAnExistingFile())
		})

		Context("when it fails to get the volume path", func() {
			BeforeEach(func() {
				Expect(os.RemoveAll(volumePath)).To(Succeed())
			})

			It("returns an error", func() {
				Expect(driver.MarkVolumeArtifacts(logger, volumeID)).To(MatchError(ContainSubstring(fmt.Sprintf("volume does not exist `%s`", volumeID))))
			})
		})

		Context("when it fails to move the volume", func() {
			BeforeEach(func() {
				Expect(os.RemoveAll(linkDirPath)).To(Succeed())
			})

			It("returns an error", func() {
				Expect(driver.MarkVolumeArtifacts(logger, volumeID)).To(MatchError(ContainSubstring(fmt.Sprintf("reading link id for volume %s/gc.%s", filepath.Dir(volumePath), volumeID))))
			})
		})

		Context("when it fails to rename the volume metadata", func() {
			BeforeEach(func() {
				Expect(os.RemoveAll(metaDirPath)).To(Succeed())
			})

			It("returns an error", func() {
				Expect(driver.MarkVolumeArtifacts(logger, volumeID)).To(MatchError(ContainSubstring("renaming volume metadata")))
			})
		})
	})

	Describe("DestroyVolume", func() {
		var (
			volumeID   string
			volumePath string
		)

		JustBeforeEach(func() {
			volumeID = randVolumeID()
			var err error
			volumePath, err = driver.CreateVolume(logger, "", volumeID)
			Expect(err).NotTo(HaveOccurred())
		})

		It("deletes the overlayxfs volume by id", func() {
			Expect(volumePath).To(BeADirectory())

			Expect(driver.DestroyVolume(logger, volumeID)).To(Succeed())
			Expect(volumePath).ToNot(BeAnExistingFile())
		})

		It("deletes the associated symlink", func() {
			Expect(volumePath).To(BeADirectory())
			linkFilePath := filepath.Join(storePath, overlayxfs.LinksDirName, volumeID)
			Expect(linkFilePath).To(BeAnExistingFile())
			linkfileinfo, err := ioutil.ReadFile(linkFilePath)
			Expect(err).ToNot(HaveOccurred())
			symlinkPath := filepath.Join(storePath, overlayxfs.LinksDirName, string(linkfileinfo))
			Expect(symlinkPath).To(BeAnExistingFile())

			Expect(driver.DestroyVolume(logger, volumeID)).To(Succeed())
			Expect(volumePath).ToNot(BeAnExistingFile())
			Expect(linkFilePath).ToNot(BeAnExistingFile())
			Expect(symlinkPath).ToNot(BeAnExistingFile())
		})

		It("deletes the metadata file", func() {
			metaFilePath := filepath.Join(storePath, store.MetaDirName, fmt.Sprintf("volume-%s", volumeID))
			Expect(ioutil.WriteFile(metaFilePath, []byte{}, 0644)).To(Succeed())
			Expect(driver.DestroyVolume(logger, volumeID)).To(Succeed())
			Expect(metaFilePath).ToNot(BeAnExistingFile())
		})

		Context("during garbage collection", func() {
			var metaFilePath string

			JustBeforeEach(func() {
				metaFilePath = filepath.Join(storePath, store.MetaDirName, fmt.Sprintf("volume-%s", volumeID))
				Expect(ioutil.WriteFile(metaFilePath, []byte{}, 0644)).To(Succeed())
				Expect(driver.MarkVolumeArtifacts(logger, volumeID)).To(Succeed())

				volumeID = "gc." + volumeID
			})

			It("deletes the metadata file", func() {
				Expect(driver.DestroyVolume(logger, volumeID)).To(Succeed())
				Expect(metaFilePath).ToNot(BeAnExistingFile())
			})
		})

		Context("when removing the metadata file fails for a reason other than the file not existing", func() {
			var metaFilePath string

			JustBeforeEach(func() {
				metaFilePath = filepath.Join(storePath, store.MetaDirName, fmt.Sprintf("volume-%s", volumeID))
				createFileEvenRootCantRemove(metaFilePath)
			})

			AfterEach(func() {
				removeFileEvenRootCantRemove(metaFilePath)
			})

			It("doesn't return an error, but logs the incident", func() {
				Expect(driver.DestroyVolume(logger, volumeID)).To(Succeed())
				Expect(logger.Buffer()).To(gbytes.Say("deleting-metadata-file-failed"))
			})
		})

		Context("when the associated symlink has already been deleted", func() {
			It("does not fail", func() {
				linkFilePath := filepath.Join(storePath, overlayxfs.LinksDirName, volumeID)
				Expect(linkFilePath).To(BeAnExistingFile())
				linkfileinfo, err := ioutil.ReadFile(linkFilePath)
				Expect(err).ToNot(HaveOccurred())
				symlinkPath := filepath.Join(storePath, overlayxfs.LinksDirName, string(linkfileinfo))
				Expect(symlinkPath).To(BeAnExistingFile())
				Expect(os.Remove(symlinkPath)).To(Succeed())

				Expect(driver.DestroyVolume(logger, volumeID)).To(Succeed())
				Expect(volumePath).ToNot(BeAnExistingFile())
			})
		})

		Context("when the associated link file has already been deleted", func() {
			It("does not fail", func() {
				linkFilePath := filepath.Join(storePath, overlayxfs.LinksDirName, volumeID)
				Expect(linkFilePath).To(BeAnExistingFile())
				Expect(os.Remove(linkFilePath)).To(Succeed())

				Expect(driver.DestroyVolume(logger, volumeID)).To(Succeed())
				Expect(volumePath).ToNot(BeAnExistingFile())
			})
		})
	})

	Describe("MoveVolume", func() {
		var (
			volumeID   string
			volumePath string
		)

		JustBeforeEach(func() {
			volumeID = randVolumeID()
			var err error
			volumePath, err = driver.CreateVolume(logger, "", volumeID)
			Expect(err).NotTo(HaveOccurred())
		})

		It("moves the volume to the given location", func() {
			newVolumePath := fmt.Sprintf("%s-new", volumePath)

			_, err := os.Stat(newVolumePath)
			Expect(err).To(HaveOccurred())
			Expect(os.IsNotExist(err)).To(BeTrue())

			err = driver.MoveVolume(logger, volumePath, newVolumePath)
			Expect(err).ToNot(HaveOccurred())
			stat, err := os.Stat(newVolumePath)
			Expect(err).ToNot(HaveOccurred())
			Expect(stat.IsDir()).To(BeTrue())
		})

		It("updates the volume link to point to the new volume location", func() {
			newVolumePath := fmt.Sprintf("%s-new", volumePath)
			_, err := os.Stat(newVolumePath)
			Expect(err).To(HaveOccurred())
			Expect(os.IsNotExist(err)).To(BeTrue())
			fileInVolume := "file-in-volume"
			filePath := filepath.Join(volumePath, fileInVolume)
			f, err := os.Create(filePath)
			Expect(err).ToNot(HaveOccurred())
			Expect(f.Close()).To(Succeed())

			err = driver.MoveVolume(logger, volumePath, newVolumePath)
			Expect(err).ToNot(HaveOccurred())

			linkName, err := ioutil.ReadFile(filepath.Join(storePath, overlayxfs.LinksDirName, filepath.Base(newVolumePath)))
			Expect(err).NotTo(HaveOccurred())
			linkPath := filepath.Join(storePath, overlayxfs.LinksDirName, string(linkName))
			_, err = os.Lstat(linkPath)
			Expect(err).NotTo(HaveOccurred())

			target, err := os.Readlink(linkPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(target).To(Equal(newVolumePath))

			_, err = os.Stat(filepath.Join(storePath, overlayxfs.LinksDirName, filepath.Base(newVolumePath)))
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when the source volume does not exist", func() {
			It("returns an error", func() {
				newVolumePath := fmt.Sprintf("%s-new", volumePath)
				err := driver.MoveVolume(logger, "nonsense", newVolumePath)
				Expect(err).To(MatchError(ContainSubstring("source volume doesn't exist")))
			})
		})

		Context("when the target volume already exists", func() {
			It("returns without error", func() {
				err := driver.MoveVolume(logger, volumePath, filepath.Dir(volumePath))
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("HandleOpaqueWhiteouts", func() {
		var (
			opaqueWhiteouts []string
			volumeID        string
			volumePath      string
		)

		JustBeforeEach(func() {
			volumeID = randVolumeID()
			var err error
			volumePath, err = driver.CreateVolume(logger, "", volumeID)
			Expect(err).NotTo(HaveOccurred())

			Expect(os.MkdirAll(filepath.Join(volumePath, "a/b"), 0755)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(volumePath, "a/b/file_1"), []byte{}, 0755)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(volumePath, "a/b/file_2"), []byte{}, 0755)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(volumePath, "c/d/e"), 0755)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(volumePath, "c/d/file_1"), []byte{}, 0755)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(volumePath, "c/d/file_2"), []byte{}, 0755)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(volumePath, "c/d/e/file_3"), []byte{}, 0755)).To(Succeed())

			opaqueWhiteouts = []string{
				"/a/b/.wh..wh..opq",
				"c/d/.wh..wh..opq",
			}
		})

		It("sets the overlay opaque dir xattr", func() {
			Expect(driver.HandleOpaqueWhiteouts(logger, volumeID, opaqueWhiteouts)).To(Succeed())

			abFolderPath := filepath.Join(volumePath, "a/b")
			files, err := ioutil.ReadDir(abFolderPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(HaveLen(2))
			xattr, err := system.Lgetxattr(abFolderPath, "trusted.overlay.opaque")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(xattr)).To(Equal("y"))

			cdFolderPath := filepath.Join(volumePath, "c/d")
			Expect(cdFolderPath).To(BeADirectory())
			files, err = ioutil.ReadDir(cdFolderPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(HaveLen(3))
			xattr, err = system.Lgetxattr(cdFolderPath, "trusted.overlay.opaque")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(xattr)).To(Equal("y"))
		})
	})

	Describe("WriteVolumeMeta", func() {
		It("creates the correct metadata file", func() {
			err := driver.WriteVolumeMeta(logger, "1234", base_image_puller.VolumeMeta{Size: kb})
			Expect(err).NotTo(HaveOccurred())

			metaFilePath := filepath.Join(storePath, store.MetaDirName, "volume-1234")
			Expect(metaFilePath).To(BeAnExistingFile())
			metaFile, err := os.Open(metaFilePath)
			Expect(err).NotTo(HaveOccurred())
			var meta base_image_puller.VolumeMeta

			Expect(json.NewDecoder(metaFile).Decode(&meta)).To(Succeed())
			Expect(meta).To(Equal(base_image_puller.VolumeMeta{Size: kb}))
		})
	})

	Describe("VolumeSize", func() {
		var (
			volumeID string
			size     int64
			sizeErr  error
		)

		BeforeEach(func() {
			volumeID = randVolumeID()
			createVolume(storePath, driver, "parent-id", volumeID, 3000)
		})

		JustBeforeEach(func() {
			size, sizeErr = driver.VolumeSize(logger, volumeID)
		})

		It("returns the volume size", func() {
			Expect(sizeErr).NotTo(HaveOccurred())
			Expect(size).To(BeEquivalentTo(3000))
		})

		Context("when metadata is missing", func() {
			BeforeEach(func() {
				Expect(os.Remove(volumeMetaPath(storePath, volumeID))).To(Succeed())
			})

			It("returns NotExist error", func() {
				Expect(os.IsNotExist(sizeErr)).To(BeTrue())
			})
		})
	})

	Describe("GenerateVolumeMeta", func() {
		var (
			volumeID              string
			generateMetadataError error
		)

		BeforeEach(func() {
			volumeID = randVolumeID()
		})

		JustBeforeEach(func() {
			generateMetadataError = driver.GenerateVolumeMeta(logger, volumeID)
		})

		Context("when the volume does not exist", func() {
			It("returns an error", func() {
				Expect(generateMetadataError).To(MatchError(ContainSubstring("volume does not exist `%s`", volumeID)))
			})
		})

		Context("when the volume exists", func() {
			BeforeEach(func() {
				createVolume(storePath, driver, "parent-id", volumeID, 3000)
			})

			It("succeeds", func() {
				Expect(generateMetadataError).NotTo(HaveOccurred())
			})

			Context("when the meta file does not exist", func() {
				BeforeEach(func() {
					Expect(os.Remove(volumeMetaPath(storePath, volumeID))).To(Succeed())
				})

				It("generates the meta file", func() {
					var generatedVolumeMeta base_image_puller.VolumeMeta
					metadataFile, err := os.Open(volumeMetaPath(storePath, volumeID))
					Expect(err).NotTo(HaveOccurred())
					Expect(json.NewDecoder(metadataFile).Decode(&generatedVolumeMeta)).To(Succeed())
					Expect(generatedVolumeMeta.Size).To(BeNumerically("~", 3000, 50))
				})
			})

			Context("when the meta file exists", func() {
				BeforeEach(func() {
					Expect(ioutil.WriteFile(volumeMetaPath(storePath, volumeID), []byte("some random stuff"), 0755)).To(Succeed())
				})

				It("regenerates the meta file", func() {
					var generatedVolumeMeta base_image_puller.VolumeMeta
					metadataFile, err := os.Open(volumeMetaPath(storePath, volumeID))
					Expect(err).NotTo(HaveOccurred())
					Expect(json.NewDecoder(metadataFile).Decode(&generatedVolumeMeta)).To(Succeed())
					Expect(generatedVolumeMeta.Size).To(BeNumerically("~", 3000, 50))
				})
			})
		})
	})
})

func randVolumeID() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("%d", r.Int())
}

func volumeMetaPath(storePath string, id string) string {
	return filepath.Join(storePath, store.MetaDirName, fmt.Sprintf("volume-%s", id))
}

func createVolume(storePath string, driver *overlayxfs.Driver, parentID, id string, size int64) string {
	path, err := driver.CreateVolume(lagertest.NewTestLogger("test"), parentID, id)
	Expect(err).NotTo(HaveOccurred())

	volumeContentsPath := filepath.Join(storePath, store.VolumesDirName, id, "contents")
	Expect(ioutil.WriteFile(volumeContentsPath, []byte{}, 0755)).To(Succeed())
	Expect(os.Truncate(volumeContentsPath, size)).To(Succeed())

	metaFilePath := volumeMetaPath(storePath, id)
	metaContents := fmt.Sprintf(`{"Size": %d}`, size)
	Expect(ioutil.WriteFile(metaFilePath, []byte(metaContents), 0644)).To(Succeed())

	return path
}

func ensureQuotaMatches(fileName string, expectedQuota int64) {
	Expect(fileName).To(BeAnExistingFile())

	contents, err := ioutil.ReadFile(fileName)
	Expect(err).NotTo(HaveOccurred())

	quota, err := strconv.ParseInt(string(contents), 10, 64)
	Expect(err).NotTo(HaveOccurred())

	Expect(quota).To(BeNumerically("~", expectedQuota, 5))
}

func createFileEvenRootCantRemove(pathname string) {
	f, err := os.OpenFile(pathname, os.O_CREATE|os.O_WRONLY, 0600)
	Expect(err).NotTo(HaveOccurred())
	f.Close()
	Expect(syscall.Mount(pathname, pathname, "", syscall.MS_BIND, "bind")).To(Succeed())
}

func removeFileEvenRootCantRemove(pathname string) {
	Expect(syscall.Unmount(pathname, 0)).To(Succeed())
	Expect(os.Remove(pathname)).To(Succeed())
}

func createVeryLongFilePath(startPath string) error {
	currentPath := startPath
	for i := 0; i < 40; i++ {
		name := ""
		for j := 0; j < 100; j++ {
			name = name + "a"
		}
		if err := mkdirAt(currentPath, name); err != nil {
			return err
		}
		currentPath = filepath.Join(currentPath, name)
	}

	return nil
}

func mkdirAt(atPath, path string) error {
	fd, err := syscall.Open(atPath, syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	return syscall.Mkdirat(fd, path, 755)
}

func mustSucceed(err error) {
	if err != nil {
		freeCmd := exec.Command("free", "-h")
		freeCmd.Stdout = GinkgoWriter
		freeCmd.Stderr = GinkgoWriter
		Expect(freeCmd.Run()).To(Succeed())
	}
	Expect(err).NotTo(HaveOccurred(), "grootfs init-store failed. This might be because the machine is "+
		"running out of RAM. Check the output of `free` above for more info.")
}

func getUidAndGid(fileInfo os.FileInfo) (int, int) {
	unixFileInfo, ok := fileInfo.Sys().(*syscall.Stat_t)
	Expect(ok).To(BeTrue(), "failed to cast fileInfo.Sys()")

	return int(unixFileInfo.Uid), int(unixFileInfo.Gid)
}
