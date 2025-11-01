
# 构建镜像
docker image rm -f go-k8s-demo; docker build -t go-k8s-demo:latest .

# 服务部署
kubectl apply -f go-deploy.yaml                      
kubectl get pods
kubectl get svc

# 访问服务
# 服务将通过 NodePort 30080 访问
# 直接访问：
curl http://127.0.0.1:30080

# 如果需要临时调试，也可以使用端口转发：
# kubectl port-forward svc/go-server-service 80:80
# 然后访问：curl http://127.0.0.1