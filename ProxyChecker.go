package main

import (
	"log"
	"time"
	"os"
	"bufio"
	"errors"
	"sync"
	"sync/atomic"
	"github.com/oschwald/geoip2-golang"
	"net"
	"strings"
	"io"
	"bytes"
	"fmt"
	"hash/fnv"
	"strconv"
	"golang.org/x/net/proxy"
)

const TIMEOUT = time.Duration(5 * time.Second)
const WORKER_THREADS = 200
//downloadable at: https://dev.maxmind.com/geoip/geoip2/geolite2/
const GEO_IP_FILE = "GeoLite2-Country.mmdb"
//ip of google
const TEST_TARGET = "http://216.58.210.14"

const CHECK_DOMAIN = "95.163.201.5:9003"
const CHECK_READ_TIMEOUT = 2
const CHECK_WRITE_TIMEOUT = 2

var REDIRECT_ERROR = errors.New("Host redirected to different target")

type logWriter struct {
}

func (writer logWriter) Write(bytes []byte) (int, error) {
	return fmt.Print(string(bytes))
}

func main() {
	log.SetFlags(0)
	log.SetOutput(new(logWriter))

	log.Println("Loading input")

	// 测试
	toTest := make(chan GopherProxy)
	// 正在检测
	working := make(chan GopherProxy, 256)

	done := make(chan bool)

	var testIndex int32 = 0
	var totalProxies int = 0

	var wg sync.WaitGroup

	for i := 0; i < WORKER_THREADS; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				// 接受待测试的
				gopherProxy, more := <-toTest
				if !more {
					break
				}

				// 原子+1, index 则 返回 [index] 为 [旧] 值
				index := atomic.AddInt32(&testIndex, 1)

				log.Printf("[%d / %d] %s %s", testIndex, totalProxies, "Testing", gopherProxy.host)
				if gopherProxy.isCheckRandom() {
					log.Println("DEBUG-", index, "Working Socks", gopherProxy.socks5, gopherProxy.host, gopherProxy.time, "ms")
					// 如果有效,册将proxy写入到working channel中
					working <- gopherProxy
				}
			}
		}()
	}

	go writeWorkingProxies(working, done)

	inputFile := os.Args[1]
	// 打开文件,读取待检测的socks5服务器列表
	input, err := os.Open(inputFile)
	if err != nil {
		log.Fatal("osOpenInputFile",err)
	}

	// 注意最后自动关闭文件
	defer input.Close()

	// 统计总行数
	totalLines, err := lineCounter(input)
	if err != nil {
		log.Fatal("lineCounter:",err)
	}

	// 待测试代理服务器总行数
	totalProxies = totalLines

	// 准备从第一个字节读取
	input.Seek(0, 0)

	// 读取ip对应的geo库文件
	var db *geoip2.Reader
	if _, err := os.Stat(GEO_IP_FILE); err == nil {
		log.Println("GEO-IP File found")
		dbFile, err := geoip2.Open(GEO_IP_FILE)
		if err != nil {
			log.Fatal("geo ip open",err)
		}

		db = dbFile
	}

	// 最后需要关闭打开的文件
	defer func() {
		if db != nil {
			db.Close()
		}
	}()

	// 打开一个新的读取流
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		// 逐行读取文件
		line := scanner.Text()
		// line内容为 host:port
		// 至读取前半截host内容
		ip := net.ParseIP(strings.Split(line, ":")[0])
		countryIso := ""
		if db != nil {
			// 查询ip数据库,获取所在国家的IsoCode
			country, err := db.Country(ip)
			if err == nil {
				countryIso = country.Country.IsoCode
			}
		}

		// 向toTest channel发送一个需要测试检测的代理服务主机
		toTest <- GopherProxy{host: line, country:countryIso}
	}

	if err = scanner.Err(); err != nil {
		log.Fatal("scanner err:",err)
	}

	// 关闭totest channel
	close(toTest)

	// 等待 协程 执行
	wg.Wait()

	//everything is written to the channel
	// 关闭 working channel
	close(working)

	// 从 done在读取一个值
	<-done
}

type GopherProxy struct {
	host    string
	country string
	socks5  bool
	time    int64
}
//
//func (gopherProxy *GopherProxy) isOnline() bool {
//	// 测试socks5,是否有效,且记录耗时
//	if works, time := testSocksProxy(gopherProxy.host, true); works {
//		gopherProxy.socks5 = true
//		gopherProxy.time = time
//		return true
//	}
//
//	// 如果socks5测试失败,则使用socks4测试, 则记录本机访问的耗时
//	if works, time := testSocksProxy(gopherProxy.host, false); works {
//		gopherProxy.socks5 = false
//		gopherProxy.time = time
//		return true
//	}
//
//	// 否则返回失败,socks5代理服务器无效,标识为不在线
//	return false
//}


func (gopherProxy *GopherProxy) isCheckRandom() bool {
	dialer,err := proxy.SOCKS5("tcp",gopherProxy.host,nil,proxy.Direct)
	if err != nil {
		log.Printf("[ERROR] %s proxy.SOCKS5 %s\r\n", gopherProxy.host, err )
		return false
	}

	conn, err := dialer.Dial("tcp", CHECK_DOMAIN)
	if err != nil {
		log.Printf("[ERROR]SOCKS5.Dial connect failed: %v\r\n", err)
		return false
	}


	conn.SetWriteDeadline(time.Now().Add(time.Duration(CHECK_WRITE_TIMEOUT * time.Second)))
	conn.SetReadDeadline(time.Now().Add(time.Duration(CHECK_READ_TIMEOUT * time.Second)))

	defer conn.Close()

	curTimestamp := strconv.FormatInt(time.Now().Unix(),10)
	nSendBytes,err := conn.Write([]byte(curTimestamp))
	if err != nil {
		log.Printf("[ERROR]SOCKS5.Dial failed: %v\r\n", err)
		return false
	}
	buffer := make([]byte, 1024)
	nReadBytes, err := conn.Read(buffer)
	if err != nil {
		log.Printf("[ERROR] conn read:%d  %v", nReadBytes, err)
		return false
	}
	if nReadBytes == nSendBytes && strings.HasPrefix(string(buffer),curTimestamp) {
		gopherProxy.socks5 = true
		return true
	}
	return false
}


func writeWorkingProxies(working <-chan GopherProxy, done chan <- bool) {
	outputFile := os.Args[2]
	if _, err := os.Stat(outputFile); err == nil {
		// path doesn't exist does not exist
		// 检测文件是否存在,如果存在,则删除文件
		os.Remove(outputFile)
	}
	// 创建文件
	output, err := os.Create(outputFile)
	if err != nil {
		// 文件创建失败,打印日志并且,直接退出
		log.Fatal("Create outputFile:",err)
	}

	// 最后自动关闭已打开的文件
	defer output.Close()

	//hash of already inserted proxies for faster compares
	uniqueMap := make(map[uint32]struct{})
	// 创建一个流写入
	writer := bufio.NewWriter(output)
	for {
		// 从 working 接收proxy待检测的
		gopherProxy, more := <-working
		if !more {
			break
		}

		// 地址
		address := gopherProxy.host
		// 对地址进行 hash 值编码,当做地址的hash值
		// addressHash 返回一个无符号数字
		addressHash := hash(address)
		// 判断 hash值是否存在 uniqueMap 中,如果已经存在,则退出
		if _, present := uniqueMap[addressHash]; present {
			break
		}

		// 对hash值创建新的结构体
		uniqueMap[addressHash] = struct{}{}
		// 向 output地址中写入有效的地址
		_, err := writer.WriteString(address + "\n")
		if err != nil {
			// 如果出错,则打印日志,且退出
			log.Fatal("output writer string:",err)
		}
	}

	// 将缓存的数据写入到硬盘中
	writer.Flush()

	// 操作完成,写入true到done中
	done <- true
}

// 计算字符串的hash值
func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
//
//// 测试socks5是否有效
//func testSocksProxy(line string, socks5 bool) (bool, int64) {
//
//	// 创建一个http的client
//	httpClient := &http.Client{
//		Transport: createSocksProxy(socks5, line),
//		Timeout: TIMEOUT,
//		CheckRedirect: func(req *http.Request, via []*http.Request) error {
//			return REDIRECT_ERROR
//		},
//	}
//
//	// 开始时间
//	start := time.Now()
//
//	// 测试socks5访问的url
//	resp, err := httpClient.Get(TEST_TARGET)
//
//	// 结束时间
//	end := time.Now()
//	responseTime := end.Sub(start).Nanoseconds() / time.Millisecond.Nanoseconds()
//	if err != nil {
//		// 检查是否为跳转
//		if urlError, ok := err.(*url.Error); ok && urlError.Err == REDIRECT_ERROR {
//			// test if we got the custom error
//			log.Println("Redirect", line)
//			return true, responseTime
//		}
//		// 失败
//		log.Printf("Failed[%s] -> %s",line, err)
//		return false, 0
//	}
//
//	defer resp.Body.Close()
//	// 成功,返回测试耗时
//	return true, responseTime
//}
//
//func createSocksProxy(socks5 bool, proxyAddr string) *http.Transport {
//	var dialSocksProxy func(string, string) (net.Conn, error)
//	if socks5 {
//		dialSocksProxy = socks.DialSocksProxy(socks.SOCKS5, proxyAddr)
//	} else {
//		dialSocksProxy = socks.DialSocksProxy(socks.SOCKS4, proxyAddr)
//	}
//
//	tr := &http.Transport{Dial: dialSocksProxy}
//	return tr;
//}

func lineCounter(r io.Reader) (int, error) {
	buf := make([]byte, 32 * 1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := r.Read(buf)
		count += bytes.Count(buf[:c], lineSep)

		switch {
		case err == io.EOF:
			return count, nil

		case err != nil:
			return count, err
		}
	}
}