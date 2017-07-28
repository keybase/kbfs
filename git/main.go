package main

import (
	"gopkg.in/src-d/go-billy.v3/osfs"
	gogit "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
)

func main() {
	dot := osfs.New("/tmp/gittest")
	s, err := filesystem.NewStorage(dot)
	if err != nil {
		panic(err)
	}
	repo, err := gogit.Init(s, nil)
	if err != nil {
		panic(err)
	}

	config := &config.RemoteConfig{
		Name: "t",
		URL:  "/tmp/testcheckout/",
	}
	_, err = repo.CreateRemote(config)
	if err != nil {
		panic(err)
	}

	o := &gogit.FetchOptions{
		RemoteName: "t",
		RefSpecs:   []config.RefSpec{"master:master"},
	}
	err = repo.Fetch(o)
	if err != nil {
		panic(err)
	}
}
