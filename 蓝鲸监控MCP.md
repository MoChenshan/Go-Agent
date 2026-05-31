蓝鲸监控 MCP 发布及IDE侧安装说明

负责人seanczxchen(陈自欣)，ctenetliu(刘易行)最后修改于03-03 19:18
1,793
334
1
0


TL; DR:


用开发者可以在Cursor或Codebody的IDE里面调用蓝鲸监控的MCP，去帮助在与代码进行关联分析问题，


使用前，请先在 权限中心申请对应MCP服务的对应业务的使用权限

国内上云环境-权限中心

海外SG环境-权限中心






安装一个合适的IDE, Cursor 或者 Codebuddy 推荐 Codebuddy

把以下的 Json 文件配置到 MCP Server 里头

访问 AccessToken获取，获取后，将对应AccessToken在上文的headers中替换即可， 记得将 Access_Token 这个单词替换为你的key，前后的引号和斜杠不要动

国内上云环境-AccessToken获取

海外SG环境-AccessToken获取



注意，由于 Access_Token 有效期 180 天， 如果重新点击上面的链接，将会被重置。而且所有的MCP Server都需要使用同一个Access Token。



国内上云环境- MCP配置



1
{
2
"mcpServers": {
3
"bkmonitorv3-prod-log-query": {
4
"headers": {
5
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
6
},
7
"type": "streamableHttp",
8
"transportType": "streamable-http",
9
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-log-query/mcp/",
10
"description": "蓝鲸观测MCP（Streamable HTTP）--日志查询服务"
11
},
12
"bkmonitorv3-prod-metrics-query": {
13
"headers": {
14
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
15
},
16
"type": "streamableHttp",
17
"transportType": "streamable-http",
18
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-metrics-query/mcp/",
19
"description": "蓝鲸观测MCP（Streamable HTTP）--指标查询服务"
20
},
21
"bkmonitorv3-prod-alarm": {
22
"headers": {
23
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
24
},
25
"type": "streamableHttp",
26
"transportType": "streamable-http",
27
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-alarm/mcp/",
28
"description": "蓝鲸观测MCP（Streamable HTTP）--告警查询服务"
29
},
30
"bkmonitorv3-prod-event-query": {
31
"headers": {
32
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
33
},
34
"type": "streamableHttp",
35
"transportType": "streamable-http",
36
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-event-query/mcp/",
37
"description": "蓝鲸观测MCP（Streamable HTTP）--事件查询服务"
38
},
39
"bkmonitorv3-prod-tracing": {
40
"headers": {
41
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
42
},
43
"type": "streamableHttp",
44
"transportType": "streamable-http",
45
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-tracing/mcp/",
46
"description": "蓝鲸观测MCP（Streamable HTTP）--APM Tracing查询"
47
},
48
"bkmonitorv3-prod-metadata-query": {
49
"headers": {
50
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
51
},
52
"type": "streamableHttp",
53
"transportType": "streamable-http",
54
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-metadata-query/mcp/",
55
"description": "蓝鲸观测MCP（Streamable HTTP）--元数据查询服务"
56
},
57
"bkmonitorv3-prod-dashboard-query": {
58
"headers": {
59
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
60
},
61
"type": "streamableHttp",
62
"transportType": "streamable-http",
63
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-dashboard-query/mcp/",
64
"description": "蓝鲸观测MCP（Streamable HTTP）--仪表盘配置查询服务"
65
},
66
"bkmonitorv3-prod-dashboard-edit": {
67
"headers": {
68
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
69
},
70
"type": "streamableHttp",
71
"transportType": "streamable-http",
72
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bkmonitorv3-prod-dashboard-edit/mcp/",
73
"description": "蓝鲸观测MCP（Streamable HTTP）--仪表盘编辑服务"
74
}
75
}
76
}


海外SG环境- MCP配置

1
{
2
"mcpServers": {
3
"bkmonitorv3-prod-log-query": {
4
"headers": {
5
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
6
},
7
"type": "streamableHttp",
8
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bkmonitorv3-prod-log-query/mcp/",
9
"transportType": "streamable-http"
10
},
11
"bkmonitorv3-prod-metrics-query": {
12
"headers": {
13
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
14
},
15
"type": "streamableHttp",
16
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bkmonitorv3-prod-metrics-query/mcp/",
17
"transportType": "streamable-http"
18
},
19
"bkmonitorv3-prod-alarm": {
20
"headers": {
21
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
22
},
23
"type": "streamableHttp",
24
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bkmonitorv3-prod-alarm/mcp/",
25
"transportType": "streamable-http"
26
},
27
"bkmonitorv3-prod-event-query": {
28
"headers": {
29
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
30
},
31
"type": "streamableHttp",
32
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bkmonitorv3-prod-event-query/mcp/",
33
"transportType": "streamable-http"
34
},
35
"bkmonitorv3-prod-metadata-query": {
36
"headers": {
37
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
38
},
39
"type": "streamableHttp",
40
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bkmonitorv3-prod-metadata-query/mcp/",
41
"transportType": "streamable-http"
42
},
43
"bkmonitorv3-prod-dashboard-query": {
44
"headers": {
45
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
46
},
47
"type": "streamableHttp",
48
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bkmonitorv3-prod-dashboard-query/mcp/",
49
"transportType": "streamable-http"
50
},
51
"bkmonitorv3-prod-dashboard-edit": {
52
"headers": {
53
"X-Bkapi-Authorization": "{\"access_token\":\"ACCESS_TOKEN\"}"
54
},
55
"type": "streamableHttp",
56
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bkmonitorv3-prod-dashboard-edit/mcp/",
57
"transportType": "streamable-http"
58
}
59
}
60
}
61
​
效果：










一、MCP 是什么？








MCP（Model Context Protocol，模型上下文协议），是一种面向AI大模型安全访问外部数据、工具、业务接口的开放协议标准。

MCP 的核心目标是为大模型与各类系统之间的集成，提供统一、安全、可控、高可扩展性的桥梁和“上下文管理”能力。它规定了：

服务发现（Discovery）：让模型有办法自动发现有哪些可用的数据和功能。

工具调用（Tool invocation）：描述每一个“工具”（数据查询、工单流转、监控操作等）的参数、权限、调用说明等，让模型能像调用本地函数一样使用外部能力。

权限与安全控制：统一鉴权模型、数据隔离、调用审计，以及防止滥用的限流约束。

标准返回格式：所有请求与响应的数据结构标准化，便于模型和系统准确理解彼此的含义。

MCP 并不是一种具体的通讯协议，而是一套对“模型-系统集成”场景的功能描述、接口约定和安全策略。通过实现 MCP Server，不同的业务/平台可以快速安全地“被模型访问”；通过 MCP Client，模型可以低成本集成越来越多的外部工具与数据，赋能更多智能场景。







简单类比







把 MCP 当成「AI 版的 USB 标准 / 电源插座标准」

各种系统（监控、工单、CMDB…）只要做成 MCP Server，模型就能用「统一的方式」访问









二、 本次蓝鲸监控 MCP server 与 AIDEV 上的MCP server 有什么区别？


区别：


本次蓝鲸的MCP是对原有AIDev上面的MCP进行封装，引入了权限管控的能力，通过一个 Token 那用户就可以访问对应的资源， 不需要复杂的权限封装



首先，安装MCP 客户端，目前大部分用户都是使用一些Vibe Coding 等工具来进行MCP的调用，

公司内部版的CodeBuddy https://codebuddy.woa.com/



公司付费的Cursor Team 版本 http://www.cursor.ai

说明：公司目前正在为大家提供Cursor Team版本的灰度功能，请务必使用Team版本，以免数据泄露。



MCP 放在IDE 优点和场景：


研发日常工作都会打开IDE , 更适合研发的使用习惯，研发也不一定需要学习蓝鲸的UI 交互界面

可以在IDE里面打关联开代码，更快进行bug处理跟根因分析 （LLM 太懂代码了）

可以用类似Vibe Coding的方式就能够达成那个用户目标， 使用起来更方便，灵活性超级强，例如可以把日志原文全部抓取下来之后，直接在IDE里面让IDE让你写一个脚本，对日志进行二次处理。

可以把整个过程、聊天、解决问题的过程一键聚沉淀成 skill 方便复用



MCP + Prompt 做成 Agent 放在AIDEV优点和场景：
不是什么时候每个人都打开着IDE。

可以对接到 企业微信机器人，使用更方便





后续我们正在研究如何把两个优点结合起来，直接在IDE里面进行一键沉淀，提交到AIDEV上面



三、MCP 能解决什么问题：站在业务 & 平台的视角


对开发 / SRE / 运维同学
大幅度减少专业知识， 更容易做「自动化场景」：排障、巡检、变更前检查、变更后验证等

更容易把多个场景进行串联。

对业务方
更快把业务系统「接入 AI」：一次接入，多场景复用

可以让「指标、仪表盘、告警、工单、变更」都被 AI 看得懂、问得动