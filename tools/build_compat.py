#!/usr/bin/env python3
"""Build a freestanding Zygisk adapter without a C++ runtime dependency."""
from pathlib import Path
import os
import subprocess
import struct
import shutil

ROOT = Path(__file__).resolve().parents[1]
CACHE = Path.home() / ".cache/hyperfont-control-build"
CLANG = os.environ.get("HFC_CLANG", shutil.which("clang++") or "clang++")
LLD = Path(os.environ.get("HFC_LLD", shutil.which("ld.lld") or "ld.lld"))

def verify_elf(path, arch, diagnostics):
    data=Path(path).read_bytes()
    assert data[:6]==b'\x7fELF\x02\x01'
    assert struct.unpack_from('<H',data,18)[0]=={'arm64':183,'x86_64':62}[arch]
    offset=struct.unpack_from('<Q',data,40)[0]
    size,count,_=struct.unpack_from('<HHH',data,58)
    sections=[struct.unpack_from('<IIQQQQIIQQ',data,offset+i*size) for i in range(count)]
    undefined=set();exported=set();needed=[]
    for section in sections:
        if section[1] not in [6,11]:continue
        strings_section=sections[section[6]]
        strings=data[strings_section[4]:strings_section[4]+strings_section[5]]
        def name(index):return strings[index:strings.index(0,index)].decode()
        for pos in range(section[4],section[4]+section[5],section[9]):
            if section[1]==6:
                tag,value=struct.unpack_from('<qQ',data,pos)
                if tag==1:needed.append(name(value))
            else:
                index,info,other,defined,_,_=struct.unpack_from('<IBBHQQ',data,pos)
                if index:(exported if defined else undefined).add(name(index))
    assert needed==['libc.so'],needed
    assert undefined<={'open','read','close'},undefined
    assert 'zygisk_module_entry' in exported,exported
    assert ('Java_local_hyperfont_FontCompatProbe_apply' in exported)==diagnostics
    phoff=struct.unpack_from('<Q',data,32)[0]
    phsize,phcount=struct.unpack_from('<HH',data,54)
    for i in range(phcount):
        kind,flags,_,_,_,_,_,alignment=struct.unpack_from('<IIQQQQQQ',data,phoff+i*phsize)
        if kind==1:
            assert not flags&1 or not flags&2,'writable executable segment'
            assert alignment>=16384

def build(output, arch="arm64", diagnostics=False):
    CACHE.mkdir(parents=True, exist_ok=True)
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    triple = {"arm64": "aarch64-linux-android29", "x86_64": "x86_64-linux-android29"}[arch]
    obj = CACHE / ("compat-" + arch + ("-diagnostic" if diagnostics else "") + ".o")
    command = [CLANG, "--target=" + triple, "-std=c++17", "-O2", "-fPIC", "-ffreestanding", "-fno-exceptions", "-fno-rtti", "-fno-threadsafe-statics", "-fno-use-cxa-atexit", "-fvisibility=hidden", "-fno-stack-protector", "-Wall", "-Wextra", "-Werror", "-c", str(ROOT / "native/font_compat.cpp"), "-o", str(obj)]
    command += ["-I", str(ROOT / "native/third_party")]
    if diagnostics:
        command.append("-DHFC_DIAGNOSTICS=1")
    subprocess.run(command, check=True)
    # A link-only import stub declares the stable Bionic APIs used here. It is
    # NEVER packaged: DT_NEEDED makes Android resolve its real libc.so instead.
    # Relying on the parent's loaded libc fails inside Android linker namespaces.
    stub_obj = CACHE / ("libc-import-" + arch + ".o")
    stub = CACHE / ("libc-import-" + arch + ".so")
    source = 'extern "C" { int open(const char*,int,...){return -1;} long read(int,void*,unsigned long){return -1;} long write(int,const void*,unsigned long){return -1;} int close(int){return -1;} }'
    subprocess.run([CLANG, "--target=" + triple, "-x", "c++", "-ffreestanding", "-fPIC", "-fno-stack-protector", "-fno-exceptions", "-fno-rtti", "-c", "-", "-o", str(stub_obj)], input=source, text=True, check=True)
    subprocess.run([str(LLD), "-shared", "-soname", "libc.so", "-o", str(stub), str(stub_obj)], check=True)
    subprocess.run([str(LLD), "-shared", "--no-undefined", "-z", "max-page-size=16384", "-z", "relro", "-z", "now", "--hash-style=both", "--build-id=sha1", "-soname", "hyperfont_compat.so", "-o", str(output), str(obj), str(stub)], check=True)
    verify_elf(output,arch,diagnostics)
    return output

if __name__ == "__main__":
    import argparse
    p=argparse.ArgumentParser()
    p.add_argument("output")
    p.add_argument("--arch", default="arm64", choices=["arm64", "x86_64"])
    p.add_argument("--diagnostics", action="store_true")
    args=p.parse_args()
    print(build(args.output, args.arch, args.diagnostics))
