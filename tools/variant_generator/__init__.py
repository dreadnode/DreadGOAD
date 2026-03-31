"""
GOAD Variant Generator Package

Tools for creating graph-isomorphic variants of GOAD with randomized entity names.
"""

from .name_generator import NameGenerator
from .goad_variant_generator import GOADVariantGenerator

__all__ = ['NameGenerator', 'GOADVariantGenerator']
