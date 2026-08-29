# Prefer official OpenWA deploy docs. Placeholder multi-stage for Render custom image if needed.
FROM node:22-bookworm
WORKDIR /app
RUN apt-get update && apt-get install -y git && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 https://github.com/rmyndharis/OpenWA.git .
RUN npm ci || npm install
ENV PORT=2785
ENV DATABASE_TYPE=sqlite
EXPOSE 2785
CMD ["npm", "start"]
