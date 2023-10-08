#!/usr/bin/env bash

rm -rf ./configs
mkdir ./configs

generate_config_toml() {
    local prefix="$1"
    local mode="$2"
    local parent="$3"
    # Add leading zeros to the prefix
    local formatted_prefix=$(printf "%02d" "$prefix")
    local output_file="./configs/${formatted_prefix}_${mode}.toml"
    
    # Add leading zeros to the prefix for ID
    local formatted_id=$(printf "%02d" "$prefix")

    cat >"$output_file" <<EOL
ConfigPort = $((6000 + prefix))
RProxyConfigPort = $((7000 + prefix))
RProxyListenAddress = "127.0.0.1"
RProxyBin = "./rproxy"

COAPPort = $((5000 + prefix))
HTTPPort = $((8000 + prefix))
GRPCPort = $((9000 + prefix))

Backend = "docker"
ID = "${mode}_${formatted_id}"

Mode = "$mode"

RegistryPort = $((4000 + prefix))
Host = "localhost"

MistifyStrategy = "leastbusy"
EOL

    if [ -n "$parent" ]; then
        echo "ParentAddress = \"$parent\"" >>"$output_file"
    fi

    echo "Configuration saved to: $output_file"
}

if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <number_of_fog_nodes> <number_of_edge_nodes_per_fog>"
    exit 1
fi

# Number of fog nodes and edge nodes per fog
num_fog_nodes="$1"
num_edge_nodes_per_fog="$2"

# Create the cloud configuration file
generate_config_toml 1 "cloud"

# Create fog nodes configuration files
for ((i = 2; i <= num_fog_nodes + 1; i++)); do
    generate_config_toml "$i" "fog" "localhost:4001"
done

# Create edge nodes configuration files
for ((i = num_fog_nodes + 2; i <= num_fog_nodes + num_fog_nodes * num_edge_nodes_per_fog + 1; i++)); do
    fog_index=$((1 + (i - num_fog_nodes - 2) / num_edge_nodes_per_fog))
    generate_config_toml "$i" "edge" "localhost:$((4000 + fog_index + 1))"
done

echo "Configuration files generated for $num_fog_nodes fog nodes and $num_edge_nodes_per_fog edge nodes per fog."
