"""
此插件用于将本服务的SSE接口接入到太极agent编排平台
"""
import json
import uuid
import requests
import asyncio
import re

from datetime import datetime
from typing import Dict, Union, List
from polaris_lite.api.consumer import ConsumerAPI
from polaris_lite.model.request import GetOneInstanceRequest

g_consumer_api = None

async def get_one_instance(service_namespace, service_name):
    # 获取实例
    request = GetOneInstanceRequest(namespace=service_namespace, service=service_name)
    instances = await ConsumerAPI.get_one_instance(request)
    if not instances:
        raise Exception("no available node")
    instance = instances.instances[0]
    return instance

class MainTool:

    def __init__(self, namespace = 'Development', service = 'trpc.magic.oncall_agent.sse', path = '/v1/agent', api_url='', token='', model = '', options = '{}'):
        self.namespace = namespace
        self.service = service
        self.service_path = path
        self.api_url = api_url
        self.token = token
        self.model = model
        self.request_params = {}
        self.options = self.parseOptions(options)

    def run(self, input: Dict[str, Dict[str, Union[str, int]]]) -> Dict[str, Dict[str, Union[str, int]]]:
        query = input["input"]["query"]
        context = self.parseContext(input)
        query_id = input.get('query_id', str(uuid.uuid4()))
        prepend = input["input"].get('prepend', None)
        startTime = datetime.now()
        result = ""
        response_text = ""

        if prepend is not None:
            result = prepend

        try:
            response = self.requestLLM(query, context, query_id, False)
            if response.status_code != 200:
                raise Exception(f"请求失败: {response.status_code} - {response.text}")

            response_text = response.text
            response_json = response.json()

            # Parse new response format
            result += response_json.get("response", "")
            
            return {
                "retcode": 0,
                "output": {
                    "result": result,
                    "query": query
                },
                "debug_info": {
                    "context_params": context
                },
                "global_output": response.json(),
                "message": ""
            }
        except Exception as e:
            return {
                "retcode": -1,
                "output": {
                    "result": str(e),
                    "query": query
                },
                "global_output": {},
                "debug_info": {
                    "context_params": context,
                    "response_text": response_text,
                    "request_params": self.request_params
                },
                "message": str(e)
            }

    def stream_run(self, input):
        query = input["input"]["query"]
        prepend = input["input"].get('prepend', None)
        context = self.parseContext(input)
        query_id = input.get('query_id', str(uuid.uuid4()))
        startTime = datetime.now()
        lastMessageTime = datetime.now()
        is_first_token = True
        answer = ""
        response_text = ""
        model_trace_id = ""
        reasoning_content = ""

        # 流数据输出前输出意图
        if prepend is not None:
            yield {
                "retcode": 0,
                "result": prepend,
                "message": ""
            }

        try:
            response = self.requestLLM(query, context, query_id, True)
            current_event = None
            
            for chunk in response.iter_lines():
                if chunk is None or chunk == b'':
                    continue

                chunk = chunk.decode('utf-8')
                
                # Parse event line
                if chunk.startswith("event:"):
                    current_event = chunk[6:].strip()
                    continue
                
                # Parse data line
                if chunk.startswith("data:"):
                    newChunk = re.sub(r'data:\s+', 'data:', chunk)
                    data = newChunk[5:]
                    if data == "" or data is None:
                        continue
                    
                    try:
                        chunkJSON = json.loads(data)
                        
                        # Check for errors
                        if chunkJSON.get('error'):
                            yield {
                                "retcode": -1,
                                "debug_info": chunkJSON,
                                "result": chunkJSON.get('error', 'Unknown error'),
                                "message": chunkJSON.get('error', 'Unknown error')
                            }
                            break

                        # Extract content from new format
                        content = chunkJSON.get('response', '')
                        is_finished = chunkJSON.get('finished', False)
                        global_output = chunkJSON.get('global_output', {})
                        
                        answer += content
                        
                        is_first_token = False
                        lastMessageTime = datetime.now()
                        
                        yield {
                            "retcode": 0,
                            "debug_info": chunkJSON,
                            "result": content,
                            "message": "",
                            "global_output": global_output
                        }
                        
                        # Break if finished
                        if is_finished:
                            break
                            
                    except json.JSONDecodeError as e:
                        yield {
                            "retcode": 0,
                            "debug_info": {
                                "error": str(e)
                            },
                            "result": str(e),
                            "message": str(e)
                        }
                
            # 输出结束帧
            yield {
                "retcode": 0,
                "result": "<finish>",
                "message": ""
            }
        except Exception as e:
            yield {
                "retcode": -1,
                "result": str(e),
                "message": str(e),
                "debug_info": {
                    "response_text": response_text,
                    "request_params": self.request_params
                }
            }


    # 解析上下文
    def parseContext(self, input: Dict[str, Dict[str, Union[str, int]]]) -> Dict[str, any]:
        context_json_string = input.get("context", "")
        try:
            context_params = json.loads(context_json_string)
            return context_params
        except json.JSONDecodeError:
            return {
                "bid": "",
                "vuid": "",
                "qimei36": ""
            }

    def requestLLM(self, query: str, context: Dict[str, str], query_id: str, stream=True):
        headers = {
            "Content-Type": "application/json;utf-8",
        }
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"

        # New request format
        # Get user from context, fallback to vuid or a default value
        user = context.get("user", context.get("vuid", "default_user"))
        
        params = {
            "user": user,
            "content": query,
            "session_id": context.get("session_id", query_id),
        }
        
        # Merge with additional options if needed
        params = {**params, **self.options}

        url = self.getServiceUrl()

        self.request_params = {
            "url": url,
            "data": params
        }
        resp = requests.post(url, headers=headers, json=params, stream=stream)
        resp.encoding = 'utf-8'
        return resp

    # 获取模型
    def getModel(self, context: Dict[str, str]):
        if self.model is not None:
            return self.model
        return context.get("model_type", "")

    def getServiceUrl(self):
        if self.api_url is not None and self.api_url != "":
            return self.api_url + self.service_path
        instance = asyncio.run(get_one_instance(self.namespace, self.service))
        return "http://" + instance.get_host() + ":"+ str(instance.get_port()) + self.service_path

    """
    解析选项字符串为字典
    :param options: 选项字符串
    :return: 选项字典
    """
    def parseOptions(self, options: str) -> Dict[str, Union[str, int]]:
        try:
            return json.loads(options)
        except json.JSONDecodeError:
            return {}

    def input_keys(self) -> List[str]:
        return ["query"]

    def output_keys(self) -> List[str]:
        return ["result"]


if __name__ == '__main__':
    mytool = MainTool(namespace="Development", service="trpc.magic.oncall_agent.sse", path="/v1/agent")  # 可编写工具类的测试代码
    input = {
        "context": '{"bid": "100000000", "vuid": "150000084", "qimei36": "150000084", "forward_cid": ""}',
    }
    input["input"] = {}
    input["input"]['query'] = '你好123321123312'
    result = mytool.run(input)
