

# 代理环境准备
MacOS M1  Docker Desktop 上 Resource Proxies 上 打开 ‘手动配置’ 并将 http 和 https 代理均设置为如下
在 http://127.0.0.1:7880 
可能需要设置 export DOCKER_BUILDKIT=0

docker exec -it warp-proxxy /bin/bash





# 构建镜像
docker image rm -f go-k8s-demo; docker build -t go-k8s-demo:latest .

# 服务部署
kubectl apply -f go-deploy.yaml                      
kubectl get pods
kubectl get svc
## 重建 pod
kubectl delete pod -l app=go-server
kubectl get pods

# 访问服务
 服务将通过 NodePort 30080 访问
 直接访问： curl http://127.0.0.1:30080

# 临时调试
如果需要临时调试，也可以使用端口转发：
 kubectl port-forward svc/go-server-service 80:80
 然后访问：curl http://127.0.0.1


# 镜像预先拉取


# coordinate / broker 的构建、部署、更新
curl -x socks5h://127.0.0.1:7880 https://www.google.com -v
docker pull --platform=linux/arm64 golang:1.22-alpine
docker pull --platform=linux/arm64 alpine:3.20
NAMESPACE=cluster-beijing; kubectl get ns $NAMESPACE >/dev/null 2>&1 || kubectl create ns $NAMESPACE
kubectl apply -f deployments/redis/deployment.yaml  -n $NAMESPACE
DOCKER_BUILDKIT=1   docker build  --pull=false  -t coordinator:latest -f deployments/coordinator/Dockerfile . \
&& kubectl apply -f deployments/coordinator/deployment.yaml -n $NAMESPACE \
&& kubectl delete pod -l app=coordinator -n $NAMESPACE; 
DOCKER_BUILDKIT=1   docker build  --pull=false  -t broker:latest -f deployments/broker/Dockerfile .  \
&& kubectl apply -f deployments/broker/deployment.yaml  -n $NAMESPACE \
&& kubectl delete pod -l  app=broker -n $NAMESPACE



# 测试命令：
curl http://127.0.0.1:30081/; echo ; curl http://127.0.0.1:30081/api/v1/brokers; echo ; curl http://127.0.0.1:30080/; echo ; 


http://127.0.0.1:30081/api/v1/brokers
http://127.0.0.1:30081/api/v1/clients

Coordinator: send message to a specific client

You can POST to the coordinator to send a message to a client. The coordinator
will look up which broker manages the client and publish a notification to the
broker's Redis channel.

Example:

```bash
curl -X POST \
	-H "Content-Type: application/json" \
	-d '{"client_id":"Client-104923AM","data":"Hello from coordinator"}' \
	http://127.0.0.1:30081/api/v1/send
```

The coordinator responds with 202 Accepted and a small JSON payload when the
message is published.


# MacOS M1 上 LB 部署
brew install nginx

配置文件 /opt/homebrew/etc/nginx/nginx.conf 
nginx will load all files in  /opt/homebrew/etc/nginx/servers/
重新加载配置：  brew services restart nginx 