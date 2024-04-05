Contributor License Agreement
---------------

Follow these steps to make a contribution to any of our open source repositories:

1. Ensure that you have completed our CLA Agreement for [individuals](https://www.cloudfoundry.org/wp-content/uploads/2015/07/CFF_Individual_CLA.pdf) or [corporations](https://www.cloudfoundry.org/wp-content/uploads/2015/07/CFF_Corporate_CLA.pdf).

1. Set your name and email (these should match the information on your submitted CLA)
  ```
  git config --global user.name "Firstname Lastname"
  git config --global user.email "your_email@example.com"
  ```

1. All contributions must be sent using GitHub pull requests as they create a nice audit trail and structured approach.

The originating github user has to either have a github id on-file with the list of approved users that have signed
the CLA or they can be a public "member" of a GitHub organization for a group that has signed the corporate CLA.
This enables the corporations to manage their users themselves instead of having to tell us when someone joins/leaves an organization. By removing a user from an organization's GitHub account, their new contributions are no longer approved because they are no longer covered under a CLA.

If a contribution is deemed to be covered by an existing CLA, then it is analyzed for engineering quality and product
fit before merging it.

If a contribution is not covered by the CLA, then the automated CLA system notifies the submitter politely that we
cannot identify their CLA and ask them to sign either an individual or corporate CLA. This happens automatically as a
comment on pull requests.

When the project receives a new CLA, it is recorded in the project records, the CLA is added to the database for the
automated system uses, then we manually make the Pull Request as having a CLA on-file.


Initial Setup
---------------
- Install docker

- Add required directories

```bash
# create parent directory
mkdir -p ~/workspace
cd ~/workspace

# clone ci
git clone https://github.com/cloudfoundry/wg-app-platform-runtime-ci.git

# clone repo
git clone https://github.com/cloudfoundry/garden-runc-release.git --recursive
cd garden-runc-release
```

- Concourse and fly cli

Running Tests
---------------

> [!IMPORTANT]
> The following scripts is ran against a Concourse worker. Set `FLY_TARGET` (Defaults to `shared`) environment variable to target your installation instead

- `./scripts/create-docker-container.bash`: This will create a docker container with appropriate mounts. This
script can be used for interactive development with a long running container. Not all tests pass in docker and that's why we recommend running tests in Concourse before submitting a PR.
- `./scripts/test-in-docker.bash`: Create docker container and run linters and template tests in a single script.
- `./scripts/test-in-concourse.bash <package>`: Create concourse on-off job and test all components or a single package. Optional `CLEAN_CACHE=yes` environment variable is recommended to be set when submitting a PR to make sure cache is cleared.

When inside docker container:

- `/repo/scripts/docker/build-binaries.bash`: (REQUIRED) This will build required binaries for running tests.
- `/repo/scripts/docker/test.bash`: This will run all tests in this repo.
- `/repo/scripts/docker/test.bash <package>`: This will only run a package's tests
- `/repo/scripts/docker/test.bash <package> <sub-package>`: This will only run sub-package tests for package
- `/repo/scripts/docker/tests-template.bash`: This will test bosh-spec templates.
- `/repo/scripts/docker/lint.bash`: This will run required linters.

> [!IMPORTANT]
> If you are about to submit a PR, please make sure to run `CLEAN_CACHE=yes ./scripts/test-in-concourse.bash` to ensure everything is tested with a rebuild of required binaries. If you are developing, you can create create a docker container first, then the only required script to run before testing your specific component is `build-binaries.bash`. Most tests for this release pass in docker .
