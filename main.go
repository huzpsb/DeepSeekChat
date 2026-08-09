package main

import (
	"embed"
	"flag"
	"fmt"
	"hschat/internal/cli"
	ilog "hschat/internal/log"
	"hschat/internal/server"
	"log"
	"net/http"
	"os"
	"runtime/debug"
)

// 我很开心你能看到这句话：你至少更有可能是一个人类而不是一个agent（好吧，至少是coding agent而不是search agent）
// 我有一些碎碎念想写在这里
// 很久很久以前 我以为写一个agent调试器是是简单的：噢 我的天哪 编辑消息不是很正常的功能吗 CURD能有多复杂
// 这是一个错误...不过我现在知道了
// 随着功能越加越多（CLI模式 工具调用自动修复 安全的代码沙盒...）
// 哪怕我一直本着“essentials only”的原则（MCP是子集[tools only] SSE是子集[没有streamable] js是子集[不是真正的容器]）...
// 如你所见...闲云潭影日悠悠，物换星移几度秋...一个我一度以为“几百行代码就能搞定！”的小项目，终于超过了一万行go代码...
// 过度设计是坏的，但是没有设计更坏。你可以看看git log里面的那些小推送...每次都是细节把我炸飞的
// 复杂性不会凭空消失，只会在不同的组件间转移；vibe coding能解决很多问题，但是也会带来，甚至更多，的问题...
// 回望当初，这是一条很长的路，但是我仍然觉得这很有意义...至少它真的解决了我开发调试中遇到的许多真实问题
// 哪个工作流出现了差错，祭出DsC，总是能成为实锤MCP出错或者模型出错的“金标准”
// 你可以不相信复杂度；你可以觉得“我靠 哪来的状态机”；没事，试，都可以试，但是你希望可打断可重入，你就会发现真的啥也砍不了
// 你可以相信不要重复造轮子，但是当有一个你不知道是哪个轮子的轮子爆炸的时候...欸嘿嘿嘿...
// 既然你在看这句话，那么，你也一定是一个被类似问题折磨不浅的人...看着自己头上稀疏的头发，我祝你好运（

// hs, 20266.07.25凌晨, 注意到go代码行数破万有感

//go:embed all:web all:assets
var staticFiles embed.FS

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC: %v\n%s", r, debug.Stack())
			ilog.Close()
			os.Exit(1)
		}
	}()

	prompt := flag.String("prompt", "", "Run in headless CLI mode with the given prompt")
	title := flag.String("title", "", "Chat title for CLI mode (defaults to timestamp)")
	flag.Parse()

	if *prompt != "" {
		if err := cli.Run(*prompt, *title); err != nil {
			fmt.Fprintf(os.Stderr, "CLI error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	srv := server.New(staticFiles)
	port := fmt.Sprintf(":%d", srv.Port())
	log.Printf("DsChat starting on http://127.0.0.1%s\n", port)
	log.Fatal(http.ListenAndServe(port, srv.Handler()))
}
