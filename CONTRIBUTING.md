# Contribution Guidelines

Thank you for your interest in this project! When contributing to this repository, please first discuss the change you wish to make via issue. 

This project has a [Code of Conduct](CODE-OF-CONDUCT.md). Please follow it in all your interactions with the project.

Your contributions will also require signoff on commits via the [Developer Certificate of Origin](https://developercertificate.org/) (DCO). When you submit a pull request, a DCO-bot will automatically determine whether you need to provide signoff for your commit. Please follow the instructions provided by DCO-bot, as pull requests cannot be merged until the author(s) have provided signoff to fulfill the DCO requirement.

## Development

To contribute to this repo you will have to create a [Merge Request](https://docs.gitlab.com/ee/user/project/merge_requests/). Here are the high-level steps: 

* Create feature branch with your changes and push it to the repo
* Use [Conventional Commit](https://www.conventionalcommits.org/en/v1.0.0) messages
* Create merge request (MR) to have your changes reviewed

## Tooling

The `Makefile` located in the root of the project provides a number of tools to make your dev loop faster/easier. Run `make` to see all the option. After that, your local dev loop looks like this: 1) making changes, 2) run either or both of these two commands:

```shell
make test
make lint
```

Before submitting MR, run all the same test/validation/scan/... that will be done in CI 

```shell
make quality
```

## Releasing 

This project uses on-git-tag based release process.

```shell
make bump-<patch|minor|major>
```

For example:

```shell
make bump-patch # v1.2.3 → v1.2.4
make bump-minor # v1.2.3 → v1.3.0 
make bump-major # v1.2.3 → v2.0.0
```

The CI pipeline will automatically kick-in and execute the release process ([.goreleaser.yaml](.goreleaser.yaml)): 

* Validate (same as on MR qualification)
* Build and SBOM [container image](https://github.com/mchmarny/gpuid/container_registry/123847)
* Create and publish GitLab [release artifacts](https://github.com/mchmarny/gpuid/-/releases)
* Distribute built image to N registries (e.g. NVCR)

## Signing Commits 

Git, by itself doesn't provide any guarantees about the author of a commit. (see [here](https://dlorenc.medium.com/should-you-sign-git-commits-f068b07e1b1f) for details). For this reasons, this repo requires signed commits. Signed commits are increasingly being required around NV, so you may as well as do it now.      

1) Start by generating a Signing-Specific SSH Key (Don’t use your existing Git transport SSH key for signing.)

```shell
ssh-keygen -t ed25519 -C "signing-key" -f ~/.ssh/id_ed25519_signing
```

2) Next, configure Git to Use SSH Key for Signing by adding the private key to your SSH agent:

```shell
ssh-add ~/.ssh/id_ed25519_signing
```

3) After that, tell Git to use this SSH key for signing:

```shell
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519_signing.pub
```

4) And, enable signing commits and tags by default:

```shell
git config --global commit.gpgsign true
git config --global tag.gpgsign true
```

5) Finally, add Public Key to GitLab by first printing the content of the key: 

```shell
cat ~/.ssh/id_ed25519_signing.pub
```

And then navigating to GitLan to add it: 

https://gitlab-master.nvidia.com/-/user_settings/ssh_keys


Now, whenever making commits append the `-S` flag and your commits will be signed and appear as verified in GitLab.

```shell
git commit -S -m "my signed commit message"  
```

## Code reviews

All submissions, including submissions by project members, require review. We use GitLab merge requests for this purposes. 
