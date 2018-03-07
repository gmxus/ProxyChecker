package main

import (
	"os"
	"log"
	"bufio"
	"io"
	"sync"
	"time"
	"strconv"
	"net/proxy"
	"strings"
	"sync/atomic"
)

const CHECK_DOMAIN = "95.163.201.5:9003"
// tcp 连接 proxy host 的超时时间(秒)
const CHECK_CONNECT_TIMEOUT = 3
// tcp 读取数据 超时时间(秒)
const CHECK_READ_TIMEOUT = 2
// tcp 发送数据 超时时间(秒)
const CHECK_WRITE_TIMEOUT = 2

// 暂时忽略
const MAX_CHANNEL_BUFFER = 20

// 最大并发数
const MAX_GO_ROUTINES = 500

// 读取成功X条,则刷新到磁盘
const MAX_FLUSH_TO_DISK = 5

type TmpProxy struct {
	host string
	socks5 bool
	time int64
}

var wg sync.WaitGroup

var maxGoRoutines int32

func main() {
	if len(os.Args) != 3 {
		log.Fatal("参数错误 \r\n[参数]./XProxyChecker inputFile.txt outputFile.txt")
	}
	inputFileName := os.Args[1]
	if !FileExists(inputFileName) {
		log.Fatalf("文件不存在 %s", inputFileName)
	}
	outputFileName := os.Args[2]
	if FileExists(outputFileName) {
		// 文件已经存在,则删除文件
		os.Remove(outputFileName)
	}

	f, err := os.Open(inputFileName)
	if err != nil {
		log.Fatalf("文件打开失败 %s %s", inputFileName, err)
	}
	defer f.Close()



	// 读取文件buffer
	inputReader := bufio.NewReader(f)

	// 从磁盘中读取代理ip写入chan
	proxyHostChan := make(chan TmpProxy,MAX_CHANNEL_BUFFER)

	// 计算总共待检测的数
	proxyTotalChan := make(chan int, 1)

	// 代理测试有效的
	proxyFinishChan := make(chan TmpProxy, MAX_CHANNEL_BUFFER)

	proxyDoneChan := make(chan bool)

	wg.Add(1)
	go func(){
		totalLine := 0
		defer wg.Done()
		for {
			inputStr, inputErr := inputReader.ReadString('\n')
			if inputErr == io.EOF {
				break
			}
			tmpHost := strings.Replace(strings.Replace(inputStr,"\r\n","",-1),"\n","",-1)
			// fmt.Printf("read [%s]\r\n",tmpHost)
			proxyHostChan <- TmpProxy{host:
				tmpHost,
			}
			totalLine++
		}
		proxyTotalChan <- totalLine
		close(proxyHostChan)
		close(proxyTotalChan)
	}()


	go saveProxyResult(outputFileName, proxyFinishChan, proxyDoneChan )


	wg.Add(1)
	go func() {
		defer wg.Done()
		for proxyItem := range proxyHostChan {
			if maxGoRoutines > MAX_GO_ROUTINES {
				log.Printf("等待协程执行....,当前总协程数已经达到 %d \r\n", maxGoRoutines)
				time.Sleep(time.Second * 1)
				continue
			}
			atomic.AddInt32(&maxGoRoutines, 1)
			wg.Add(1)
			// log.Printf("-->--启动协程检测 : %s %d", proxyItem.host, maxGoRoutines)
			go func(curProxy TmpProxy) {
				defer func() {
					atomic.AddInt32(&maxGoRoutines, -1)
					wg.Done()
				}()
				if ok, elapsedTime := curProxy.isCheckRandom(); ok {
					curProxy.socks5 = true
					curProxy.time = elapsedTime
					proxyFinishChan <- curProxy
					log.Printf("testProxy %v %s time:%v",
						curProxy.socks5,
						curProxy.host,
						curProxy.time / (1000 * 1000))
				}
			}(proxyItem)
		}
	}()



	wg.Wait()
	log.Println("Start Save proxy Result...." )

	log.Printf("proxy Success Total line:%d\r\n", <- proxyTotalChan )

	log.Println("finished...")

	close(proxyFinishChan)
	<- proxyDoneChan
}

func saveProxyResult(fileName string, proxyFinishChan <- chan TmpProxy, proxyDoneChan chan <- bool ) {

	if _, err := os.Stat(fileName); err == nil {
		// path doesn't exist does not exist
		// 检测文件是否存在,如果存在,则删除文件
		os.Remove(fileName)
	}
	// 创建文件
	outputFile, err := os.Create(fileName)
	if err != nil {
		// 文件创建失败,打印日志并且,直接退出
		log.Fatal("Create outputFile:",err)
	}

	defer outputFile.Close()

	buffWriter := bufio.NewWriter(outputFile)

	allProxyHost := make(map[string]int)

	tmpIdx := 0
	for {
		finishProxy,ok := <- proxyFinishChan
		if !ok {
			log.Println(" proxyFinishChan is finished....")
			break
		}
		_, ok = allProxyHost[finishProxy.host]
		if !ok {
			// 不存在
			allProxyHost[finishProxy.host]++
			// 写入磁盘
			_, err = buffWriter.WriteString(finishProxy.host + "\n")
			if err != nil {
				log.Printf("[ERRORX]buffWriter writer string: %s",err)
				break
			}
			log.Printf("正在写入磁盘 %s", finishProxy.host)

			tmpIdx++

			// 每写入20条,则刷新到磁盘上
			if tmpIdx > 0 && tmpIdx % MAX_FLUSH_TO_DISK == 0 {
				buffWriter.Flush()
			}
		}
	}

	buffWriter.Flush()

	proxyDoneChan <- true
	log.Println("******saveProxyResult*******")
}

func FileExists(fileName string) bool {
	_, err := os.Stat(fileName)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}


func (tmpProxy *TmpProxy) isCheckRandomX() (bool,int64) {
	return true, 2
}

func (tmpProxy *TmpProxy) isCheckRandom() (bool,int64) {

	// log.Printf("[DEBUG] wait for proxy.SOCKS5...%s", tmpProxy.host)

	startTime := time.Now()
	dialer,err := proxy.SOCKS5("tcp",tmpProxy.host,nil,proxy.Direct,
		CHECK_READ_TIMEOUT * time.Second,
		CHECK_WRITE_TIMEOUT * time.Second,
	)
	if err != nil {
		log.Printf("[ERROR1] %s proxy.SOCKS5 %s\r\n", tmpProxy.host, err )
		return false,0
	}

	// log.Printf("****host:[%s] len:%d",tmpProxy.host, len(tmpProxy.host))

	// log.Printf("[DEBUG] wait for dialer.Dial...%s", tmpProxy.host)

	connDealLine := time.Duration( CHECK_CONNECT_TIMEOUT * time.Second )

	// fmt.Printf("conn timeout :%d \r\n", connDealLine)

	conn, err := dialer.DialTimeout("tcp", CHECK_DOMAIN, connDealLine)

	if err != nil {
		// log.Printf("[ERROR2]SOCKS5.Dial connect %s, failed: %s\r\n", tmpProxy.host, err)
		return false,0
	}

	defer conn.Close()

	conn.SetWriteDeadline(time.Now().Add(time.Duration(CHECK_WRITE_TIMEOUT * time.Second)))
	conn.SetReadDeadline(time.Now().Add(time.Duration(CHECK_READ_TIMEOUT * time.Second)))

	// log.Printf("[DEBUG] wait for conn.Write...%s", tmpProxy.host)

	curTimestamp := strconv.FormatInt(time.Now().Unix(),10)
	nSendBytes,err := conn.Write([]byte(curTimestamp))
	if err != nil {
		log.Printf("[ERROR]SOCKS5.Dial failed: %v\r\n", err)
		return false,0
	}
	buffer := make([]byte, 128)
	// log.Printf("[DEBUG] wait for read buffer...%s", tmpProxy.host)
	nReadBytes, err := conn.Read(buffer)
	if err != nil {
		log.Printf("[ERROR] conn read:%d  %v", nReadBytes, err)
		return false,0
	}

	finishTime := time.Now()

	elapsedTime := finishTime.Sub(startTime).Nanoseconds()

	if nReadBytes == nSendBytes && strings.HasPrefix(string(buffer),curTimestamp) {
		tmpProxy.socks5 = true
		log.Printf("[XXDEBUG] %s success", tmpProxy.host)
		return true, elapsedTime
	}
	return false,0
}