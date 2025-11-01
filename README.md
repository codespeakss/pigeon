
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
# 服务将通过 NodePort 30080 访问
# 直接访问：
curl http://127.0.0.1:30080

# 如果需要临时调试，也可以使用端口转发：
# kubectl port-forward svc/go-server-service 80:80
# 然后访问：curl http://127.0.0.1


# 清理
kubectl delete deployment broker
kubectl delete deployment go-server

kubectl delete service go-server-service



# broker 的构建、部署、更新
docker build -t broker:latest -f deployments/broker/Dockerfile . ; kubectl apply -f deployments/broker/deployment.yaml; kubectl delete pod -l app=broker

# coordinate 的构建、部署、更新
docker build -t coordinator:latest -f deployments/coordinator/Dockerfile . ; kubectl apply -f deployments/coordinator/deployment.yaml; kubectl delete pod -l app=coordinator

添加功能 ： 在 broker 启动时 自动访问 coordinator 的接口 得到 coordinator 分配给 broker的全局唯一 id