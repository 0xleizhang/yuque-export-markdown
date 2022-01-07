package main

import (
	"flag"
	"fmt"
	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/wujiyu115/yuqueg"
	"io/ioutil"
	"sort"
	"sync"
)

const IMG_DOMAIN = "https://cdn.nlark.com/yuque"

type Node struct {
	ParentId string
	Id       string
	Child    []*Node
	Data     yuqueg.RepoTocData
}

type Job struct {
	SavePath string
	Data     yuqueg.RepoTocData
}

func (n *Node) addSub(node *Node) {
	if n.Child == nil {
		n.Child = make([]*Node, 0)
	}
	n.Child = append(n.Child, node)
}
func treeify(toc []yuqueg.RepoTocData) []*Node {
	nodes := make([]*Node, 0)
	m := make(map[string]*Node)
	for _, t := range toc {
		n := Node{
			ParentId: t.ParentUUID,
			Id:       t.UUID,
			Data:     t,
		}
		nodes = append(nodes, &n)
		m[t.UUID] = &n
	}
	hm := make(map[string]*Node)
	for i, t := range nodes {
		var mmdChild *Node
		if hmt, ok := hm[t.Id]; ok {
			mmdChild = hmt
		} else {
			mmdChild = nodes[i]
			hm[t.Id] = nodes[i]
		}
		var mmdParent *Node
		if mmt, ok := hm[t.ParentId]; ok {
			mmdParent = mmt
		} else {
			if mmdParent, ok = m[t.ParentId]; ok {
				hm[t.ParentId] = mmdParent
			}
		}

		if mmdParent != nil {
			mmdParent.addSub(mmdChild)
		}

	}

	res := make([]*Node, 0)
	for _, node := range hm {
		if node.ParentId == "" {
			res = append(res, node)
		}
	}
	return res
}

func main() {
	token := ""
	ns := ""
	flag.StringVar(&token, "token", "", "")
	flag.StringVar(&ns, "ns", "", "")
	fmt.Printf("using token: %s", token)
	if token == "" {
		panic("token must setting")
	}
	yu := yuqueg.NewService(token)
	toc, err := yu.Repo.GetToc(ns)
	if err != nil {
		panic(err.Error())
	}
	tree := treeify(toc.Data)
	jobc := make(chan Job, 10)
	done := make(chan struct{})
	go startParse(jobc, tree)
	go startDownload(jobc, done, yu, ns)
	<-done
}

func startDownload(jobc <-chan Job, done chan struct{}, yu *yuqueg.Service, ns string) {
	wg := sync.WaitGroup{}
	for job := range jobc {
		wg.Add(1)
		j := job
		go func() {
			doDownload(j, yu, ns)
			wg.Done()
		}()
	}
	fmt.Printf("下载结束")
	wg.Wait()
	done <- struct{}{}
}
func doDownload(job Job, yu *yuqueg.Service, ns string) {
	fmt.Printf("%s \n", job.SavePath)
	doc, err := yu.Doc.Get(ns, job.Data.Slug, &yuqueg.DocGet{Raw: 1})
	if err != nil {
		fmt.Errorf(err.Error())
		return
	}
	var html string
	if doc.Data.BodyDraft != "" {
		html = doc.Data.BodyDraft
	} else {
		html = doc.Data.BodyHTML
	}
	md, err := convert(html)
	if err != nil {
		fmt.Errorf(err.Error())
	}
	ioutil.WriteFile(job.SavePath, []byte(md), 0644)
}

func startParse(jobc chan<- Job, tree []*Node) {
	doParse(jobc, tree, ".")
	close(jobc)
}
func doParse(jobc chan<- Job, tree []*Node, parentPath string) {
	sort.Slice(tree, func(i, j int) bool {
		return tree[i].Data.Id > tree[j].Data.Id
	})
	for i, _ := range tree {
		node := tree[i]
		savePath := parentPath + "/" + node.Data.Title
		jobc <- Job{
			SavePath: savePath,
			Data:     node.Data,
		}
		if node.Child != nil { //树的深度优先遍历
			doParse(jobc, node.Child, savePath)
		}
	}
}

func convert(html string) (string, error) {
	converter := md.NewConverter(IMG_DOMAIN, true, nil)
	return converter.ConvertString(html)
}
