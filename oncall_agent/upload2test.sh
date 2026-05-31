#!/bin/bash
#以下配置信息需要修改为你当前服务的信息
env=test
app=magic
server=oncall_agent
# 优先从git配置中获取用户名
user=$(git config user.name | awk -F ' ' '{print $1}')
# 用户名为空，兜底
if [ -z $user ]; then
    user=youngjin
fi
#删除上次编出来的二进制文件
rm ${server}
#编译构建生成二进制文件(不需修改)
go build -ldflags '-linkmode "external" -extldflags "-static"' -o ${server}
#发布命令,确保你的dtools所在目录位于PATH环境变量中
dtools bpatch -env "${env}" -app "${app}" -server "${server}" -bin "${server}" -user "${user}" -lang=go
