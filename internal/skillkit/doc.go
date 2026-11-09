// Package skillkit installs, lists and removes agent skills in a project.
//
// It is an in-tree fork of github.com/ka2n/skills-go (commit
// 56d1a362d6228a543092da8ad6e763de6414d269), reduced to what Nosey uses and
// corrected to match vercel-labs/skills 1.7.6 byte for byte on disk: the
// agent table, copy rules, folder hash and skills-lock.json format.
// See README.md for the provenance of each file.
//
// Every destination write and removal goes through os.Root and refuses a
// symlinked ancestor that leaves the workspace with a *PolicyError.
package skillkit
