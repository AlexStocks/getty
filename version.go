/******************************************************
# DESC       : getty version
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-08-17 11:23
# FILE       : version.go
******************************************************/

package getty

const (
	Version     = "0.4.05"
	DATE        = "2016/10/21"
	GETTY_MAJOR = 0
	GETTY_MINOR = 4
	GETTY_BUILD = 5
)

/*
 几个可能的优化点：
 1 session.go:Session中的rQ & wQ 均是channel实现，其本质是bounded queue，所以可能导致读写阻塞，
   如果想让write更快一点，可以使用list作为wQ的二级队列(或者称之为优先级队列)，以存放优先级比较高的且不能丢弃(在wQ full的情况下)的pkg；
 2 codec.go:(EventListener)OnMessage其实可以一次处理多个session.go:Session{rQ}的多个包，如：
   const MAX = 32
   for {
       select {
           case inPkg = <-this.rQ:
               if flag {
                  var pkgArray []interface{}
                  pkgArray = append(pkgArray, inPkg)
                  Read:
                  for i := 0; i < MAX; i++ {
                      case inPkg = <-this.rQ:
                          pkgArray = append(pkgArray, inPkg)
                      default:
                          break Read
                  }
			      this.listener.OnMessages(this, pkgArray)
			      this.incReadPkgCount(len(pkgArray))
			   } else {
			       log.Info("[session.handleLoop] drop readin package{%#v}", inPkg)
			   }
*/
