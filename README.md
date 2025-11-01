
# 构建镜像
docker image rm -f go-k8s-demo; docker build -t go-k8s-demo:latest .

# 服务部署
kubectl apply -f go-deploy.yaml                      
kubectl get pods
kubectl get svc

# 转发 用于调试
sudo kubectl port-forward svc/go-server-service 80:80