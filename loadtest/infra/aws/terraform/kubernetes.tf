# Create the kubernetes-dashboard namespace
resource "kubernetes_namespace" "kubernetes_dashboard" {
  metadata {
    name = "kubernetes-dashboard"
  }
}

# Deploy Kubernetes Dashboard using Helm
resource "helm_release" "kubernetes_dashboard" {
  name       = "kubernetes-dashboard"
  repository = "https://kubernetes.github.io/dashboard/"
  chart      = "kubernetes-dashboard"
  namespace  = kubernetes_namespace.kubernetes_dashboard.metadata[0].name
  version    = "6.0.8"

  set {
    name  = "metricsScraper.enabled"
    value = "true"
  }

  set {
    name  = "service.type"
    value = "ClusterIP" # Default: only accessible through proxy
  }

  # Uncomment to expose via LoadBalancer instead of proxy
  # set {
  #   name  = "service.type"
  #   value = "LoadBalancer"
  # }
  # 
  # set {
  #   name  = "service.externalPort"
  #   value = "443"
  # }
  #
  # set {
  #   name  = "service.loadBalancerSourceRanges"
  #   value = "{your-ip-address/32}"  # Optional IP restriction
  # }

  depends_on = [
    aws_eks_node_group.main
  ]
}

# Create admin service account for dashboard access
resource "kubernetes_service_account" "dashboard_admin" {
  metadata {
    name      = "admin-user"
    namespace = kubernetes_namespace.kubernetes_dashboard.metadata[0].name
  }
  depends_on = [
    helm_release.kubernetes_dashboard
  ]
}

# Bind admin role to the service account
resource "kubernetes_cluster_role_binding" "dashboard_admin" {
  metadata {
    name = "admin-user"
  }
  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = "cluster-admin"
  }
  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.dashboard_admin.metadata[0].name
    namespace = kubernetes_namespace.kubernetes_dashboard.metadata[0].name
  }
  depends_on = [
    kubernetes_service_account.dashboard_admin
  ]
}

# # Install nginx-ingress controller
# resource "helm_release" "nginx_ingress" {
#   name             = "nginx-ingress"
#   repository       = "https://kubernetes.github.io/ingress-nginx"
#   chart            = "ingress-nginx"
#   namespace        = "ingress-nginx"
#   create_namespace = true

#   depends_on = [aws_eks_node_group.main]
# }
