# todo list #
---

- 1 session.go:Session中的rQ & wQ 均是channel实现，其本质是bounded queue，会导致读写阻塞   
如果想让write更快一点，可以使用list作为wQ的二级队列(或者称之为优先级队列)，以存放优先级比较高的且不能丢弃(在wQ full的情况下)的pkg；
