FROM node:22-alpine
WORKDIR /app
COPY internal/portal/package*.json .
RUN npm install
COPY internal/portal .
CMD ["npx", "vite", "dev", "--host"]
EXPOSE 3334
