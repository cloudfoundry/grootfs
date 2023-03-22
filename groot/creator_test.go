package groot_test

import (
	"errors"
	"io/ioutil"
	"net/url"
	"os"

	"code.cloudfoundry.org/grootfs/groot"
	"code.cloudfoundry.org/grootfs/groot/grootfakes"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Creator", func() {
	var (
		baseImageUrl          *url.URL
		fakeImageManager      *grootfakes.FakeImageManager
		fakeBaseImagePuller   *grootfakes.FakeBaseImagePuller
		fakeLocksmith         *grootfakes.FakeLocksmith
		fakeDependencyManager *grootfakes.FakeDependencyManager
		fakeMetricsEmitter    *grootfakes.FakeMetricsEmitter
		fakeCleaner           *grootfakes.FakeCleaner
		lockFile              *os.File

		creator *groot.Creator
		logger  lager.Logger

		layerInfos    []groot.LayerInfo
		baseImageInfo groot.BaseImageInfo
		pullError     error
	)

	BeforeEach(func() {
		baseImageUrl, _ = url.Parse("/path/to/image")

		fakeImageManager = new(grootfakes.FakeImageManager)
		fakeBaseImagePuller = new(grootfakes.FakeBaseImagePuller)
		fakeLocksmith = new(grootfakes.FakeLocksmith)
		fakeDependencyManager = new(grootfakes.FakeDependencyManager)
		fakeMetricsEmitter = new(grootfakes.FakeMetricsEmitter)
		fakeCleaner = new(grootfakes.FakeCleaner)

		var err error
		lockFile, err = ioutil.TempFile("", "")
		Expect(err).NotTo(HaveOccurred())

		fakeLocksmith.LockReturns(lockFile, nil)

		logger = lagertest.NewTestLogger("creator")

		fakeImageManager.CreateReturns(groot.ImageInfo{
			Path:   "/path/to/images/123",
			Rootfs: "/path/to/images/123/rootfs",
		}, nil)

		layerInfos = []groot.LayerInfo{
			groot.LayerInfo{ChainID: "id-1"},
			groot.LayerInfo{ChainID: "id-2"},
		}
		baseImageInfo = groot.BaseImageInfo{
			LayerInfos: layerInfos,
			Config: specsv1.Image{
				Author: "Groot",
			},
		}

		pullError = nil

		creator = groot.IamCreator(
			fakeImageManager, fakeBaseImagePuller, fakeLocksmith,
			fakeDependencyManager, fakeMetricsEmitter,
			fakeCleaner)
	})

	JustBeforeEach(func() {
		fakeBaseImagePuller.FetchBaseImageInfoReturns(baseImageInfo, nil)
		fakeBaseImagePuller.PullReturns(pullError)
	})

	AfterEach(func() {
		Expect(os.Remove(lockFile.Name())).To(Succeed())
	})

	Describe("Create", func() {
		It("acquires the global lock", func() {
			_, err := creator.Create(logger, groot.CreateSpec{
				BaseImageURL: baseImageUrl,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeLocksmith.LockCallCount()).To(Equal(1))
			Expect(fakeLocksmith.LockArgsForCall(0)).To(Equal(groot.GlobalLockKey))
		})

		Context("when cleaning up store is requested", func() {
			It("cleans the store", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL:                baseImageUrl,
					CleanOnCreate:               true,
					CleanOnCreateThresholdBytes: int64(250000),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(fakeCleaner.CleanCallCount()).To(Equal(1))
				_, cacheSize := fakeCleaner.CleanArgsForCall(0)
				Expect(cacheSize).To(Equal(int64(250000)))
			})

			Context("and fails to clean up", func() {
				BeforeEach(func() {
					fakeCleaner.CleanReturns(true, errors.New("failed to clean up store"))
				})

				It("returns an error", func() {
					_, err := creator.Create(logger, groot.CreateSpec{
						BaseImageURL:  baseImageUrl,
						CleanOnCreate: true,
					})
					Expect(err).To(MatchError(ContainSubstring("failed to clean up store")))
				})
			})
		})

		It("pulls the image", func() {
			uidMappings := []groot.IDMappingSpec{groot.IDMappingSpec{HostID: 2, NamespaceID: 0, Size: 1}}
			gidMappings := []groot.IDMappingSpec{groot.IDMappingSpec{HostID: 3, NamespaceID: 0, Size: 1}}

			_, err := creator.Create(logger, groot.CreateSpec{
				BaseImageURL: baseImageUrl,
				UIDMappings:  uidMappings,
				GIDMappings:  gidMappings,
			})
			Expect(err).NotTo(HaveOccurred())

			_, actualBaseImageInfo, imageSpec := fakeBaseImagePuller.PullArgsForCall(0)
			Expect(baseImageInfo).To(Equal(actualBaseImageInfo))
			Expect(imageSpec.UIDMappings).To(Equal(uidMappings))
			Expect(imageSpec.GIDMappings).To(Equal(gidMappings))
			Expect(imageSpec.OwnerUID).To(Equal(2))
			Expect(imageSpec.OwnerGID).To(Equal(3))
		})

		It("makes an image", func() {

			uidMappings := []groot.IDMappingSpec{groot.IDMappingSpec{HostID: 50, NamespaceID: 0, Size: 1}}
			gidMappings := []groot.IDMappingSpec{groot.IDMappingSpec{HostID: 60, NamespaceID: 0, Size: 1}}
			_, err := creator.Create(logger, groot.CreateSpec{
				ID:           "some-id",
				BaseImageURL: baseImageUrl,
				UIDMappings:  uidMappings,
				GIDMappings:  gidMappings,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeImageManager.CreateCallCount()).To(Equal(1))
			_, createImagerSpec := fakeImageManager.CreateArgsForCall(0)
			Expect(createImagerSpec).To(Equal(groot.ImageSpec{
				ID:            "some-id",
				BaseVolumeIDs: []string{"id-1", "id-2"},
				BaseImage: specsv1.Image{
					Author: "Groot",
				},
				OwnerUID: 50,
				OwnerGID: 60,
			}))
		})

		It("releases the global lock", func() {
			_, err := creator.Create(logger, groot.CreateSpec{
				BaseImageURL: baseImageUrl,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeLocksmith.UnlockCallCount()).To(Equal(1))
			Expect(fakeLocksmith.UnlockArgsForCall(0)).To(Equal(lockFile))
		})

		It("returns the image", func() {
			expectedImage := groot.ImageInfo{
				Path:   "/path/to/image",
				Rootfs: "rootfs-path",
			}
			fakeImageManager.CreateReturns(expectedImage, nil)

			image, err := creator.Create(logger, groot.CreateSpec{})
			Expect(err).NotTo(HaveOccurred())
			Expect(image).To(Equal(expectedImage))
		})

		It("emits metrics for creation", func() {
			_, err := creator.Create(logger, groot.CreateSpec{
				ID:           "some-id",
				BaseImageURL: baseImageUrl,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeMetricsEmitter.TryEmitDurationFromCallCount()).To(Equal(1))
			_, name, start := fakeMetricsEmitter.TryEmitDurationFromArgsForCall(0)
			Expect(name).To(Equal(groot.MetricImageCreationTime))
			Expect(start).NotTo(BeZero())
		})

		Describe("store ownership", func() {
			It("is defined by the root ID mappings", func() {
				uidMappings := []groot.IDMappingSpec{
					groot.IDMappingSpec{HostID: 50, NamespaceID: 0, Size: 1},
					groot.IDMappingSpec{HostID: 51, NamespaceID: 1, Size: 100},
				}
				gidMappings := []groot.IDMappingSpec{
					groot.IDMappingSpec{HostID: 60, NamespaceID: 0, Size: 1},
					groot.IDMappingSpec{HostID: 61, NamespaceID: 1, Size: 300},
				}
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
					UIDMappings:  uidMappings,
					GIDMappings:  gidMappings,
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(fakeBaseImagePuller.PullCallCount()).To(Equal(1))
				_, _, imageSpec := fakeBaseImagePuller.PullArgsForCall(0)
				Expect(imageSpec.OwnerUID).To(Equal(50))
				Expect(imageSpec.OwnerGID).To(Equal(60))

				Expect(fakeImageManager.CreateCallCount()).To(Equal(1))
				_, createImagerSpec := fakeImageManager.CreateArgsForCall(0)
				Expect(createImagerSpec.OwnerUID).To(Equal(50))
				Expect(createImagerSpec.OwnerGID).To(Equal(60))
			})

			Context("when there's no root mapping", func() {
				It("sets the current user as the store owner", func() {
					_, err := creator.Create(logger, groot.CreateSpec{
						BaseImageURL: baseImageUrl,
					})
					Expect(err).NotTo(HaveOccurred())

					Expect(fakeBaseImagePuller.PullCallCount()).To(Equal(1))
					_, _, imageSpec := fakeBaseImagePuller.PullArgsForCall(0)
					Expect(imageSpec.OwnerUID).To(Equal(os.Getuid()))
					Expect(imageSpec.OwnerGID).To(Equal(os.Getgid()))

					Expect(fakeImageManager.CreateCallCount()).To(Equal(1))
					_, createImagerSpec := fakeImageManager.CreateArgsForCall(0)
					Expect(createImagerSpec.OwnerUID).To(Equal(os.Getuid()))
					Expect(createImagerSpec.OwnerGID).To(Equal(os.Getgid()))
				})
			})
		})

		Context("when the id already exists", func() {
			BeforeEach(func() {
				fakeImageManager.ExistsReturns(true, nil)
			})

			It("returns an error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
					ID:           "some-id",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeImageManager.CreateCallCount()).To(Equal(0))
				Expect(err).To(MatchError(ContainSubstring("image for id `some-id` already exists")))
			})

			It("does not pull the image", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
					ID:           "some-id",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeBaseImagePuller.PullCallCount()).To(Equal(0))
				Expect(err).To(MatchError(ContainSubstring("image for id `some-id` already exists")))
			})
		})

		Context("when checking if the id exists fails", func() {
			BeforeEach(func() {
				fakeImageManager.ExistsReturns(false, errors.New("Checking if the image ID exists"))
			})

			It("returns an error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeImageManager.CreateCallCount()).To(Equal(0))
				Expect(err).To(MatchError(ContainSubstring("Checking if the image ID exists")))
			})

			It("does not pull the image", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeBaseImagePuller.PullCallCount()).To(Equal(0))
				Expect(err).To(MatchError(ContainSubstring("Checking if the image ID exists")))
			})
		})

		Context("when the id contains invalid characters", func() {
			It("returns an error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
					ID:           "some/id",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeImageManager.CreateCallCount()).To(Equal(0))
				Expect(err).To(MatchError(ContainSubstring("id `some/id` contains invalid characters: `/`")))
			})
		})

		Context("when acquiring the lock fails", func() {
			BeforeEach(func() {
				fakeLocksmith.LockReturns(nil, errors.New("failed to lock"))
			})

			It("returns the error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
				})
				Expect(err).To(MatchError(ContainSubstring("failed to lock")))
			})

			It("does not pull the image", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeBaseImagePuller.PullCallCount()).To(BeZero())
			})
		})

		Context("when pulling the image fails", func() {
			BeforeEach(func() {
				pullError = errors.New("failed to pull image")
			})

			It("returns the error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
				})
				Expect(err).To(MatchError(ContainSubstring("failed to pull image")))
			})

			It("does not create a image", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					BaseImageURL: baseImageUrl,
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeImageManager.CreateCallCount()).To(Equal(0))
			})
		})

		Context("when cloning the image fails", func() {
			BeforeEach(func() {
				fakeImageManager.CreateReturns(groot.ImageInfo{}, errors.New("Failed to make image"))
			})

			It("returns the error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{})
				Expect(err).To(MatchError("making image: Failed to make image"))
			})
		})

		Context("when registering dependencies fails", func() {
			BeforeEach(func() {
				fakeDependencyManager.RegisterReturns(errors.New("failed to register dependencies"))
			})

			It("returns an errors", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					ID:           "my-image",
					BaseImageURL: baseImageUrl,
				})

				Expect(err).To(MatchError(ContainSubstring("failed to register dependencies")))
			})

			It("destroys the image", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					ID:           "my-image",
					BaseImageURL: baseImageUrl,
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeImageManager.DestroyCallCount()).To(Equal(1))
			})
		})

		Context("when disk limit is given", func() {
			It("passes the disk limit to the imageManager", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					ID:           "some-id",
					DiskLimit:    int64(1024),
					BaseImageURL: baseImageUrl,
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(fakeImageManager.CreateCallCount()).To(Equal(1))
				_, createImagerSpec := fakeImageManager.CreateArgsForCall(0)
				Expect(createImagerSpec).To(Equal(groot.ImageSpec{
					ID:            "some-id",
					BaseVolumeIDs: []string{"id-1", "id-2"},
					BaseImage: specsv1.Image{
						Author: "Groot",
					},
					OwnerUID:  os.Getuid(),
					OwnerGID:  os.Getgid(),
					DiskLimit: int64(1024),
				}))
			})
		})
	})
})
