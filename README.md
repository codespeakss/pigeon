

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


# 清理
kubectl delete deployment broker
kubectl delete deployment go-server

kubectl delete service go-server-service


# coordinate 的构建、部署、更新
curl -x socks5h://127.0.0.1:7880 https://www.google.com -v
docker build -t coordinator:latest -f deployments/coordinator/Dockerfile . && kubectl apply -f deployments/coordinator/deployment.yaml && kubectl delete pod -l app=coordinator


docker pull golang:1.22-alpine

# broker 的构建、部署、更新
docker build -t broker:latest -f deployments/broker/Dockerfile . ; kubectl apply -f deployments/broker/deployment.yaml; kubectl delete pod -l app=broker


# 维护： 重建 redis （删除数据）
kubectl delete pod -l app=redis



# 测试命令：
curl http://127.0.0.1:30081/; echo ; curl http://127.0.0.1:30081/api/v1/brokers; echo ; curl http://127.0.0.1:30080/; echo ; 


# MacOS M1 上 LB 部署
brew install nginx

配置文件 /opt/homebrew/etc/nginx/nginx.conf 
nginx will load all files in  /opt/homebrew/etc/nginx/servers/
重新加载配置：  brew services restart nginx 