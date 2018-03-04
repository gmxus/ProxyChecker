package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"time"
)

func handleConnection(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 4096)
	reader := bufio.NewReaderSize(conn, 4096)
	writer := bufio.NewWriterSize(conn, 4096)

	for {

		conn.SetDeadline(time.Now().Add(time.Millisecond * 10))
		n, err := reader.Read(buf)
		conn.SetDeadline(time.Time{})
		if err != nil {
			nerr, ok := err.(net.Error)
			if ok {
				if nerr.Timeout() {
					//fmt.Println("read timeout n ", n, time.Now())
					time.Sleep(time.Millisecond * 10)
					continue
				} else if nerr.Temporary() {
					fmt.Println("read error: ", err)
					return
				} else {
					fmt.Println("read error: ", err)
					return
				}

			} else {
				//fmt.Println("read error: ", err)
				return
			}
		}

		if n <= 0 {
			time.Sleep(time.Millisecond * 10)
			continue
		}

		//val := string(buf[0:n])
		//fmt.Println("val: ", val)

		for pos := 0; pos < n; pos += 64 {
			end := pos + 64
			if end > n {
				end = n
			}
			_, err := writer.Write(buf[pos:end])
			if err != nil {
				fmt.Println("write error: ", err)
				return
			}

			//fmt.Println("write n: ", n)
		}
		writer.Flush()
	}
}

func main() {
	ln, err := net.Listen("tcp", ":9003")
	if err != nil {
		panic(err)
	}

	fmt.Println("listen 9003 ok")

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal("get client connection error: ", err)
		}

		go handleConnection(conn)
	}
}
