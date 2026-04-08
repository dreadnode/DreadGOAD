#!/usr/bin/env bash

py=python3
venv="$HOME/.goad/.venv"
requirement_file="requirements.yml"

if [ ! -d "$venv" ]; then
    version=$($py --version 2>&1 | awk '{print $2}')
    echo "Python version in use : $version"
    version_numeric=$(echo "$version" | awk -F. '{printf "%d%02d%02d\n", $1, $2, $3}')
    if [ "$version_numeric" -ge 30800 ]; then
        echo 'python version >= 3.8 ok'
        if [ "$version_numeric" -lt 31100 ]; then
            requirement_file="requirements.yml"
        else
            requirement_file="requirements_311.yml"
        fi
    else
        echo "Python version is < 3.8 please update python before install"
        exit
    fi

    if $py -m venv -h 2> /dev/null | grep -qi 'usage:'; then
        echo "venv module is installed. continue"
    else
        echo "venv module is not installed."
        echo "please install $py-venv according to your system"
        echo "exit"
        exit 0
    fi

    echo '[+] venv not found, start python venv creation'
    mkdir -p ~/.goad
    $py -m venv "$venv"
    if source "$venv"/bin/activate; then
        $py -m pip install --upgrade pip
        export SETUPTOOLS_USE_DISTUTILS=stdlib
        $py -m pip install -r "$requirement_file"
        pushd ansible > /dev/null || exit
        ansible-galaxy install -r "$requirement_file"
        popd > /dev/null || exit
    else
        echo "Error in venv creation"
        rm -rf "$venv"
        exit 0
    fi
fi

source "$venv"/bin/activate
$py goad.py "$@"
deactivate
