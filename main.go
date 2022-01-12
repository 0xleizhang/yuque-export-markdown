package main

import (
	"context"
	"flag"
	"fmt"
	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/wujiyu115/yuqueg"
	"golang.org/x/sync/semaphore"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	IMG_DOMAIN = ""
	IMG_REG    = "https?://.+\\.(jpg|gif|png)"
	//https://www.yuque.com/yuque/developer/api#5b3a1535
	MaxConcurrency = 20
	Duration       = 1.4
	SaveRootPath   = "/Users/seven/Desktop/kb"
)

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

var (
	token string
	ns    string
)

func main() {

	flag.StringVar(&token, "token", "", "token")
	flag.StringVar(&ns, "ns", "", "owner/repo")
	flag.Parse()
	if token == "" {
		panic("token must setting")
	}
	fmt.Printf("using token: %s", token)
	//step :1
	yu := yuqueg.NewService(token)
	toc, err := yu.Repo.GetToc(ns)
	if err != nil {
		panic(err.Error())
	}
	tree := treeify(toc.Data)

	//step :2
	jobc := make(chan Job, 100000)
	go buildJob(jobc, tree)

	//stop: 3

	startDownload(jobc, yu, ns)

}

func startDownload(jobc <-chan Job, yu *yuqueg.Service, ns string) {
	err := os.MkdirAll(ns, os.ModePerm)
	if err != nil {
		panic(err)
	}
	//防止main协程退出
	wg := sync.WaitGroup{}
	//并发控制
	sem := semaphore.NewWeighted(MaxConcurrency)
	runChan := make(chan struct{})
	go func() {
		for {
			//限流
			runChan <- struct{}{}
			d, _ := time.ParseDuration("1.4s")
			time.Sleep(d)
		}
	}()

	for {
		select {
		case <-runChan:
			if err := sem.Acquire(context.Background(), 1); err == nil {
				job, _ := <-jobc
				wg.Add(1)
				go func() {
					//fire download
					doDownload(job, yu, ns)
					wg.Done()
					sem.Release(1)
				}()
			} else {
				fmt.Println(err.Error())
			}
		}
	}
	fmt.Println("下载结束")
	wg.Wait()
}
func doDownload(job Job, yu *yuqueg.Service, ns string) {
	fmt.Printf("start download: %s \n", job.SavePath)
	doc, err := yu.Doc.Get(ns, job.Data.Slug, &yuqueg.DocGet{Raw: 1})
	if err != nil {
		fmt.Printf("fetch doc api error: %s", err.Error())
		return
	}
	var html string
	if doc.Data.BodyDraft != "" {
		html = doc.Data.BodyDraft
	} else {
		html = doc.Data.BodyHTML
	}
	markdown, err := convertHTML2Markdown(html)

	mdPath := job.SavePath[:strings.LastIndex(job.SavePath, "/")]
	replaceMd := downloadImgAndReplace(markdown, mdPath)
	if err != nil {
		fmt.Printf("convert error: %s", err.Error())
	}
	if err := ioutil.WriteFile(job.SavePath, []byte(replaceMd), 0644); err != nil {
		fmt.Printf("write error %s", err.Error())
	}
	fmt.Printf("download success : %s \n", job.SavePath)
}

func downloadImgAndReplace(markdown string, mdPath string) string {
	reg := regexp.MustCompile(IMG_REG)
	imgs := reg.FindAllString(markdown, -1)
	fmt.Printf("find pics :%v\n", imgs)
	for i, _ := range imgs {
		img := imgs[i]
		_ = os.MkdirAll(mdPath+"/assert", os.ModePerm)
		p := "assert/" + GetUrlFileName(img)
		if err := DownloadFile(mdPath+"/"+p, img); err != nil {
			_ = fmt.Errorf(err.Error())
		} else {
			strings.Replace(markdown, img, p, -1)
			fmt.Printf("download pic : %s => %s\n", img, p)
		}
	}
	return markdown
}

func GetUrlFileName(imgUrl string) string {
	u, e := url.Parse(imgUrl)
	if e != nil {
		fmt.Errorf(e.Error())
		return imgUrl
	}
	return u.Path[strings.LastIndex(u.Path, "/")+1:]
}

func DownloadFile(filepath string, url string) error {

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}

func buildJob(jobc chan<- Job, tree []*Node) {
	defer close(jobc)
	doParse(jobc, tree, SaveRootPath)
}

func doParse(jobc chan<- Job, tree []*Node, parentPath string) {
	sort.Slice(tree, func(i, j int) bool {
		return tree[i].Data.Id > tree[j].Data.Id
	})
	for i, _ := range tree {
		node := tree[i]
		if node.Child != nil { //树的深度优先遍历
			savePath := parentPath + "/" + node.Data.Title
			err := os.MkdirAll(savePath, os.ModePerm)
			if err != nil {
				panic(err)
			}
			jobc <- Job{
				SavePath: savePath + "/" + node.Data.Title + ".md",
				Data:     node.Data,
			}
			doParse(jobc, node.Child, savePath)
		} else {
			jobc <- Job{
				SavePath: parentPath + "/" + node.Data.Title + ".md",
				Data:     node.Data,
			}
		}
	}
}

func convertHTML2Markdown(html string) (string, error) {
	converter := md.NewConverter(IMG_DOMAIN, true, nil)
	return converter.ConvertString(html)
}
