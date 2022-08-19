# gnet & getty

## 概览

8c 16g Mac
![](./mac.png)

## script

```bash
tcpkali --workers 4 -c 100 -T 300s -m "PING{$nl}" 127.0.0.1:$4
```

## version

`github.com/panjf2000/gnet v1.1.5`

`github.com/apache/dubbo-getty v1.4.8`

## Comparision

| 连接数 | 执行时间(s) | 消息长度（byte） | Gnet总发送数据量(MB) | Getty总发送数据量（MB）| Gnet总接受数据量（MB）| Getty总接受数据量（MB) | Gnet带宽（Mbps）|  Getty带宽（Mbps）|
| :--- | :---- | :--- | :--- |:--- | :---- | :--- | :--- |:--- | 
| 10 | 30 |  6 |  46558 |  6809 | 46536  | 6804  |  13008.115↓, 13014.275↑  | 1902.656↓, 1904.007↑   |
| 100 | 300 | 6  | 317931  | 69128  | 317929  | 69138  | 8889.853↓, 8889.894↑  | 1933.210↓, 1932.910↑  | 

## Conclusion

* 1 连接较少时，Gnet 总体吞吐是 Getty 的 4-6 倍左右
*  2  但在增加并发数的时候，Gnet性能下降明显，Getty 表现稳定，总体吞吐也从 6 倍下降到 4 倍左右
*  3  连接数 200 个链接的时候 gnet 就不可用了，机器直接 gg，getty 仍在稳定运行

