package main

import (
	"fmt"
	"github.com/jessevdk/go-flags"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"sealdice-core/api"
	"sealdice-core/dice"
	"syscall"
	"time"
)

/**
二进制目录结构:
data/configs
data/extensions
data/logs

extensions/
*/

func main() {
	var opts struct {
		Install                bool `short:"i" long:"install" description:"安装为系统服务"`
		Uninstall              bool `long:"uninstall" description:"删除系统服务"`
		ShowConsole            bool `long:"show-console" description:"Windows上显示控制台界面"`
		MultiInstanceOnWindows bool `short:"m" long:"multi-instance" description:"允许在Windows上运行多个海豹"`
	}

	_, err := flags.ParseArgs(&opts, os.Args)
	if err != nil {
		return
	}

	if opts.Install {
		serviceInstall(true)
		return
	}

	if opts.Uninstall {
		serviceInstall(false)
		return
	}

	if !opts.ShowConsole || opts.MultiInstanceOnWindows {
		hideWindow()
	}

	if !opts.MultiInstanceOnWindows && TestRunning() {
		return
	}

	cwd, _ := os.Getwd()
	fmt.Printf("%s %s\n", dice.APPNAME, dice.VERSION)
	fmt.Println("工作路径: ", cwd)

	diceManager := &dice.DiceManager{}

	go trayInit()

	os.MkdirAll("./data", 0644)
	MainLoggerInit("./data/main.log", true)

	cleanUp := func() {
		logger.Info("程序即将退出，进行清理……")
		err := recover()
		if err != nil {
			logger.Errorf("异常: %v 堆栈: %v", err, string(debug.Stack()))
		}

		for _, i := range diceManager.Dice {
			i.Save(true)
		}
		for _, i := range diceManager.Dice {
			i.DB.Close()
		}
		diceManager.Help.Close()
		diceManager.Save()
	}
	defer cleanUp()

	// 初始化核心
	diceManager.LoadDice()
	diceManager.TryCreateDefault()
	diceManager.InitDice()

	//a, d, err := myDice.ExprEval("7d12k4", nil)
	//if err == nil {
	//	fmt.Println(a.Parser.GetAsmText())
	//	fmt.Println(d)
	//	fmt.Println("DDD"+"#{a}", a.TypeId, a.Value, d, err)
	//} else {
	//	fmt.Println("DDD2", err)
	//}

	//runtime := quickjs.NewRuntime()
	//defer runtime.Free()
	//
	//context := runtime.NewContext()
	//defer context.Free()

	//globals := context.Globals()

	// Test evaluating template strings.

	//result, err := context.Eval("`Hello world! 2 ** 8 = ${2 ** 8}.`")
	//fmt.Println("XXXXXXX", result, err)

	// 强制清理机制
	go (func() {
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
		select {
		case <-interrupt:
			time.Sleep(time.Duration(7 * time.Second))
			logger.Info("7s仍未关闭，稍后强制退出……")
			cleanUp()
			time.Sleep(time.Duration(3 * time.Second))
			os.Exit(0)
		}
	})()

	for _, d := range diceManager.Dice {
		go diceServe(d)
	}

	uiServe(diceManager)
}

func diceServe(d *dice.Dice) {
	if len(d.ImSession.EndPoints) == 0 {
		d.Logger.Infof("未检测到任何帐号，请先到“帐号设置”进行添加")
	}

	for _, conn := range d.ImSession.EndPoints {
		if conn.Enable {
			if conn.Platform == "QQ" {
				pa := conn.Adapter.(*dice.PlatformAdapterQQOnebot)
				dice.GoCqHttpServe(d, conn, pa.InPackGoCqHttpPassword, pa.InPackGoCqHttpProtocol, true)
				time.Sleep(10 * time.Second) // 稍作等待再连接
			}

			go dice.DiceServe(d, conn)
			//for {
			//	conn.DiceServing = true
			//	// 骰子开始连接
			//	d.Logger.Infof("开始连接 onebot 服务，帐号 <%s>(%d)", conn.Nickname, conn.UserId)
			//	ret := d.ImSession.Serve(index)
			//
			//	if ret == 0 {
			//		break
			//	}
			//
			//	d.Logger.Infof("onebot 连接中断，将在15秒后重新连接，帐号 <%s>(%d)", conn.Nickname, conn.UserId)
			//	time.Sleep(time.Duration(15 * time.Second))
			//}
		}
	}
}

func uiServe(myDice *dice.DiceManager) {
	logger.Info("即将启动webui")
	// Echo instance
	e := echo.New()

	// Middleware
	//e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		Skipper:      middleware.DefaultSkipper,
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, "token"},
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete},
	}))

	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:         "1; mode=block",
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "SAMEORIGIN",
		HSTSMaxAge:            3600,
		ContentSecurityPolicy: "default-src 'self' 'unsafe-inline'; img-src 'self' data:;",
	}))
	// X-Content-Type-Options: nosniff
	e.Static("/", "./frontend")

	api.Bind(e, myDice)
	e.HideBanner = true // 关闭banner，原因是banner图案会改变终端光标位置

	exec.Command(`cmd`, `/c`, `start`, `http://localhost:3211`).Start()
	fmt.Println("如果浏览器没有自动打开，请手动访问:")
	fmt.Println("http://localhost:3211")
	e.Start(myDice.ServeAddress) // 默认:3211

	//interrupt := make(chan os.Signal, 1)
	//signal.Notify(interrupt, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	//
	//for {
	//	select {
	//	case <-interrupt:
	//		fmt.Println("主动关闭")
	//		return
	//	}
	//}
}

//
//func checkCqHttpExists() bool {
//	if _, err := os.Stat("./go-cqhttp"); err == nil {
//		return true
//	}
//	return false
//}
