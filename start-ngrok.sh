#!/bin/bash

echo "🔄 Iniciando ngrok en puerto 8080..."

# Verificar si ngrok está instalado
if ! command -v ngrok &> /dev/null; then
    echo "❌ ngrok no está instalado. Instalalo con: brew install ngrok"
    exit 1
fi

# Iniciar ngrok
ngrok http 8080 --url https://gale-unhailable-nonderogatively.ngrok-free.app