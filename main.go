package main

import (
	"context"
	"flag"
	"fmt"
	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/seven4x/yuqueg"
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
	ImgDomain = ""
	ImgReg    = "\\(https?://.+\\.(jpg|gif|png)\\S*\\)"
	// MaxConcurrency https://www.yuque.com/yuque/developer/api#5b3a1535
	MaxConcurrency = 20
	Duration       = "1.4s"
)

var (
	token  string
	ns     string
	target string
)

func main() {

	flag.StringVar(&token, "token", "", "token")
	flag.StringVar(&ns, "ns", "", "owner/repo")
	flag.StringVar(&target, "target", "/Users/seven/Desktop/kb2", "save path")
	flag.Parse()
	if token == "" {
		panic("token must setting")
	}
	fmt.Printf("using token: %s \n", token)
	//step :1获取 列表树结构
	yu := yuqueg.NewService(token)
	toc, err := yu.Repo.GetToc(ns)
	if err != nil {
		panic(err.Error())
	}
	tree := treeify(toc.Data)

	err = os.MkdirAll(target, os.ModePerm)
	if err != nil {
		panic(err)
	}

	//step :2 构建下载任务
	jobc := make(chan Job, 100000)
	go buildJob(jobc, tree)

	//step: 3
	startDownload(jobc, yu, ns)

	fmt.Print("下载结束\n")

}

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

//list convert to tree
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

func startDownload(jobc <-chan Job, yu *yuqueg.Service, ns string) {

	//防止main协程退出
	wg := sync.WaitGroup{}
	//并发控制
	sem := semaphore.NewWeighted(MaxConcurrency)
	runChan := make(chan struct{})
	d, _ := time.ParseDuration(Duration)
	go func() {
		for {
			//限流
			runChan <- struct{}{}
			time.Sleep(d)
		}
	}()
	for {
		select {
		case <-runChan:
			if err := sem.Acquire(context.Background(), 1); err == nil {
				job, ok := <-jobc
				if ok {
					wg.Add(1)
					go func() {
						//fire download
						doDownloadDoc(job, yu, ns)
						wg.Done()
						sem.Release(1)
					}()
				} else {
					fmt.Printf("下载完成 \n")
					wg.Wait()
					return
				}
			} else {
				fmt.Println(err.Error())
			}

		}
	}

}

func doDownloadDoc(job Job, yu *yuqueg.Service, ns string) {
	fmt.Printf("start download: %s \n", job.SavePath)
	doc, err := yu.Doc.Get(ns, job.Data.Slug, &yuqueg.DocGet{Raw: 1})
	if err != nil {
		fmt.Printf("fetch %s doc,error: %s \n", job.SavePath, err.Error())
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
		fmt.Printf("convert error: %s \n", err.Error())
	}
	if err := ioutil.WriteFile(job.SavePath, []byte(replaceMd), 0644); err != nil {
		fmt.Printf("write error %s \n", err.Error())
	}
	fmt.Printf("download success : %s \n", job.SavePath)
}

func downloadImgAndReplace(markdown string, mdPath string) string {
	reg := regexp.MustCompile(ImgReg)
	imgs := reg.FindAllString(markdown, -1)
	fmt.Printf("find pics :%v\n", imgs)
	for i, _ := range imgs {
		img := imgs[i]
		imgUrl := imgs[i][1 : len(imgs[i])-1]
		_ = os.MkdirAll(mdPath+"/assert", os.ModePerm)
		p := "assert/" + GetUrlFileName(imgUrl)
		if err := DownloadFile(mdPath+"/"+p, imgUrl); err != nil {
			_ = fmt.Errorf(err.Error())
		} else {
			markdown = strings.Replace(markdown, imgUrl, p, -1)
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
	doParse(jobc, tree, target)
	fmt.Printf("解析任务完成。\n")
}

func doParse(jobc chan<- Job, tree []*Node, parentPath string) {
	sort.Slice(tree, func(i, j int) bool {
		return tree[i].Data.Id > tree[j].Data.Id
	})
	for i, _ := range tree {
		node := tree[i]
		title := strings.Replace(strings.TrimSpace(node.Data.Title), "/", "-", -1)
		if node.Child != nil { //树的深度优先遍历
			savePath := parentPath + "/" + title
			err := os.MkdirAll(savePath, os.ModePerm)
			if err != nil {
				panic(err)
			}
			jobc <- Job{
				SavePath: savePath + "/" + title + ".md",
				Data:     node.Data,
			}
			doParse(jobc, node.Child, savePath)
		} else {
			jobc <- Job{
				SavePath: parentPath + "/" + title + ".md",
				Data:     node.Data,
			}
		}
	}
}

func convertHTML2Markdown(html string) (string, error) {
	converter := md.NewConverter(ImgDomain, true, nil)
	return converter.ConvertString(html)
}
