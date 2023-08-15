#!/bin/bash

read -sp "Honeycomb Token": token

sed "s/your_key_here/$token/" .env.example > .env
sed "s/your_key_here/$token/" ./secondary/.env.example > ./secondary/.env
sed "s/your_key_here/$token/" ./grpc-server/.env.example > ./grpc-server/.env


