package main

import (
       "fmt"
       
	"gopkg.in/src-d/go-billy.v3/osfs"
	gogit "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
)

func main() {
	dot := osfs.New("gittest")
	s, err := filesystem.NewStorage(dot)
	if err != nil {
		panic(err)
	}
	fmt.Println("HERE1")
	repo, err := gogit.Open(s, nil)
	if err != nil {
		panic(err)
	}

	fmt.Println("HERE2")

/**
	c := &config.RemoteConfig{
		Name: "t",
		URL:  "gitcheckout",
	}
	_, err = repo.CreateRemote(c)
	if err != nil {
		panic(err)
	}
*/

	o := &gogit.FetchOptions{
		RemoteName: "t",
		RefSpecs:   []config.RefSpec{"refs/heads/master:refs/heads/master"},
	}
	err = repo.Fetch(o)
	if err != nil {
		panic(err)
	}
}
